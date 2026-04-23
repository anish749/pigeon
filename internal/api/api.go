package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	goslack "github.com/slack-go/slack"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/hub"
	slacklistener "github.com/anish749/pigeon/internal/listener/slack"
	walistener "github.com/anish749/pigeon/internal/listener/whatsapp"
	"github.com/anish749/pigeon/internal/outbox"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
	"github.com/anish749/pigeon/internal/store/modelv1"
	"github.com/anish749/pigeon/internal/syncstatus"
)

// WhatsAppSender holds everything needed to send a WhatsApp message.
type WhatsAppSender struct {
	Client   *whatsmeow.Client
	Acct     account.Account
	Resolver *walistener.Resolver
}

// SlackSender holds everything needed to send a Slack message.
type SlackSender struct {
	BotAPI    *goslack.Client // bot token client (default for sends)
	UserAPI   *goslack.Client // user token client (--as-user sends)
	Resolver  *slacklistener.Resolver
	Messages  *slacklistener.MessageStore
	Acct      account.Account
	BotName   string // the bot's display name
	BotUserID string // the bot's Slack user ID
	UserName  string // the authenticated user's display name
	UserID    string // the authenticated user's Slack user ID
}

// Server is the daemon's HTTP API server.
type Server struct {
	mu          sync.RWMutex
	whatsapp    map[string]*WhatsAppSender // account slug → sender
	slack       map[string]*SlackSender    // account slug → sender
	gws         map[string]struct{}        // account slug → present
	hub         *hub.Hub
	outbox      *outbox.Outbox
	store       store.Store
	syncTracker *syncstatus.Tracker
	version     string
	startedAt   time.Time
}

// NewServer creates a new API server.
func NewServer(h *hub.Hub, ob *outbox.Outbox, s store.Store, version string, syncTracker *syncstatus.Tracker) *Server {
	return &Server{
		whatsapp:    make(map[string]*WhatsAppSender),
		slack:       make(map[string]*SlackSender),
		gws:         make(map[string]struct{}),
		hub:         h,
		outbox:      ob,
		store:       s,
		syncTracker: syncTracker,
		version:     version,
		startedAt:   time.Now(),
	}
}

// RegisterWhatsApp registers a WhatsApp client for sending.
func (s *Server) RegisterWhatsApp(sender *WhatsAppSender) {
	s.mu.Lock()
	s.whatsapp[sender.Acct.NameSlug()] = sender
	s.mu.Unlock()
}

// RegisterSlack registers a Slack client for sending.
func (s *Server) RegisterSlack(sender *SlackSender) {
	s.mu.Lock()
	s.slack[sender.Acct.NameSlug()] = sender
	s.mu.Unlock()
}

// RegisterGWS registers a GWS account for status reporting.
func (s *Server) RegisterGWS(acct account.Account) {
	s.mu.Lock()
	s.gws[acct.NameSlug()] = struct{}{}
	s.mu.Unlock()
}

// UnregisterGWS removes a GWS account from status reporting.
func (s *Server) UnregisterGWS(acct account.Account) {
	s.mu.Lock()
	delete(s.gws, acct.NameSlug())
	s.mu.Unlock()
}

// Start starts the HTTP server on a unix domain socket. Blocks until ctx is cancelled.
func (s *Server) Start(ctx context.Context, socketPath string) error {
	// Clean up stale socket if no one is listening.
	if _, err := net.Dial("unix", socketPath); err != nil {
		os.Remove(socketPath)
	}

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", socketPath, err)
	}

	obHandler := outbox.NewHandler(s.outbox, s.executeSend, s.hub.NotifySession)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/send", s.handleSend)
	mux.HandleFunc("POST /api/react", s.handleReact)
	mux.HandleFunc("POST /api/delete", s.handleDeleteMsg)
	mux.HandleFunc("GET /api/events", s.hub.SSEHandler())
	mux.HandleFunc("GET /api/outbox", obHandler.HandleList)
	mux.HandleFunc("POST /api/outbox/action", obHandler.HandleAction)
	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("GET /api/session/connected", s.handleSessionConnected)

	srv := &http.Server{
		Handler: mux,
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
	}

	go func() {
		<-ctx.Done()
		srv.Close()
		ln.Close()
	}()

	slog.InfoContext(ctx, "api server started", "socket", socketPath)
	err = srv.Serve(ln)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// SendRequest is the daemon API payload for /api/send.
type SendRequest struct {
	Platform string `json:"platform"`
	Account  string `json:"account"`
	Message  string `json:"message"` // Slack mrkdwn formatted text

	// Target — platform-specific, exactly one must be set.
	Slack   *SlackTarget `json:"slack,omitempty"`
	Contact string       `json:"contact,omitempty"` // WhatsApp contact name or phone

	Thread    string      `json:"thread,omitempty"`
	Broadcast bool        `json:"broadcast,omitempty"`
	PostAt    string      `json:"post_at,omitempty"` // Unix timestamp — schedule instead of send immediately
	Via       modelv1.Via `json:"via,omitempty"`
	DryRun    bool        `json:"dry_run,omitempty"`
	Force     bool        `json:"force,omitempty"`
	// SessionID identifies the Claude session that originated the send, so
	// approve/feedback actions in the outbox TUI can be delivered back to
	// the right session. Set automatically by the CLI when
	// PIGEON_SESSION_ID is in the environment. Empty for direct CLI use —
	// the send still goes through the outbox, but feedback has no session
	// to deliver to and the TUI will disable that action.
	SessionID string `json:"session_id,omitempty"`
}

// SendResponse is the daemon API response for /api/send.
type SendResponse struct {
	OK                 bool   `json:"ok"`
	Timestamp          string `json:"timestamp,omitempty"`
	ScheduledMessageID string `json:"scheduled_message_id,omitempty"` // returned when post_at is set
	Error              string `json:"error,omitempty"`
	ChannelID          string `json:"channel_id,omitempty"`   // resolved channel ID (dry-run)
	ChannelName        string `json:"channel_name,omitempty"` // resolved channel name (dry-run)
	SendAs             string `json:"send_as,omitempty"`      // sender identity
	OutboxID           string `json:"outbox_id,omitempty"`
}

func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	var req SendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, SendResponse{Error: "invalid JSON: " + err.Error()})
		return
	}

	if req.Platform == "" || req.Account == "" || req.Message == "" {
		writeJSON(w, http.StatusBadRequest, SendResponse{Error: "platform, account, and message are required"})
		return
	}
	if err := validateTarget(req.Platform, req.Slack, req.Contact); err != nil {
		writeJSON(w, http.StatusBadRequest, SendResponse{Error: err.Error()})
		return
	}

	// Default Via when not set (e.g. TUI, API callers that omit it).
	if req.Via == "" {
		req.Via = modelv1.ViaPigeonAsBot
	}

	if err := s.checkMPDMBotAccess(r.Context(), req); err != nil {
		writeJSON(w, http.StatusBadRequest, SendResponse{Error: err.Error()})
		return
	}

	// Resolve message and target so the outbox/TUI has the final text and
	// human-readable destination names.
	var resolvedMessage string
	if req.Platform == "slack" {
		resolvedMessage = ResolveSlackMessage(req.Message)
	} else {
		resolvedMessage = req.Message
	}

	var resolvedSlack *ResolvedSlackTarget
	if req.Platform == "slack" && req.Slack != nil {
		acct := account.New(req.Platform, req.Account)
		s.mu.RLock()
		sender, ok := s.slack[acct.NameSlug()]
		s.mu.RUnlock()
		if !ok {
			writeJSON(w, http.StatusBadRequest, SendResponse{Error: fmt.Sprintf("no Slack workspace %q registered", acct.Display())})
			return
		}
		var err error
		resolvedSlack, err = resolveSlackDisplay(r.Context(), sender, req.Slack)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, SendResponse{Error: err.Error()})
			return
		}
	}
	resolved := ResolvedSendRequest{SendRequest: req, ResolvedSlack: resolvedSlack, ResolvedMessage: resolvedMessage}

	// All real sends go through the outbox for human review. Dry-run is
	// the one exception — it validates targeting without sending, so
	// queuing it for approval would be meaningless.
	if !req.DryRun {
		payload, err := json.Marshal(resolved)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, SendResponse{Error: "marshal send request: " + err.Error()})
			return
		}
		item := s.outbox.Submit(req.SessionID, payload)
		slog.Info("outbox item submitted", "id", item.ID, "session_id", req.SessionID)
		writeJSON(w, http.StatusOK, SendResponse{OK: true, OutboxID: item.ID})
		return
	}

	resp := s.dispatchSend(r.Context(), resolved)
	status := http.StatusOK
	if !resp.OK {
		status = http.StatusInternalServerError
	}
	writeJSON(w, status, resp)
}

func (s *Server) sendWhatsApp(ctx context.Context, acct account.Account, req SendRequest) SendResponse {
	s.mu.RLock()
	sender, ok := s.whatsapp[acct.NameSlug()]
	s.mu.RUnlock()
	if !ok {
		return SendResponse{Error: fmt.Sprintf("no WhatsApp account %q registered", acct.Display())}
	}

	// Resolve contact query to JID.
	recipientJID, err := sender.Resolver.FindJID(ctx, req.Contact)
	if err != nil {
		var ambErr *walistener.AmbiguousContactError
		if errors.As(err, &ambErr) {
			return SendResponse{Error: formatAmbiguousContacts(ambErr, sender.Acct)}
		}
		return SendResponse{Error: fmt.Sprintf("resolve contact: %v", err)}
	}

	// Send the message.
	resp, err := sender.Client.SendMessage(ctx, recipientJID, &waE2E.Message{
		Conversation: proto.String(req.Message),
	})
	if err != nil {
		return SendResponse{Error: fmt.Sprintf("send: %v", err)}
	}

	// Ensure .meta.json exists for this conversation.
	convDir := sender.Resolver.ConvDir(ctx, recipientJID)
	displayName := sender.Resolver.ContactName(ctx, recipientJID)
	if recipientJID.Server == types.GroupServer {
		displayName = sender.Resolver.GroupName(ctx, recipientJID)
	}
	waMeta := sender.Resolver.ConvMeta(ctx, recipientJID, displayName)
	if _, err := s.store.WriteMetaIfNotExists(sender.Acct, convDir, waMeta); err != nil {
		slog.ErrorContext(ctx, "write meta failed", "conv", convDir, "error", err)
	}

	// Store locally.
	senderName := "me"
	var senderID string
	if sender.Client.Store.ID != nil {
		myJID := types.NewJID(sender.Client.Store.ID.User, types.DefaultUserServer)
		senderName = sender.Resolver.ContactName(ctx, myJID)
		senderID = myJID.String()
	}
	line := modelv1.Line{
		Type: modelv1.LineMessage,
		Msg: &modelv1.MsgLine{
			ID:       resp.ID,
			Ts:       resp.Timestamp,
			Sender:   senderName,
			SenderID: senderID,
			Via:      modelv1.ViaPigeonAsUser, // WhatsApp always sends as the user — there is no bot identity.
			Text:     req.Message,
		},
	}
	if err := s.store.Append(sender.Acct, convDir, line); err != nil {
		slog.ErrorContext(ctx, "failed to store sent message", "error", err)
	}

	return SendResponse{OK: true, Timestamp: resp.Timestamp.Format(time.RFC3339)}
}

func (s *Server) sendSlack(ctx context.Context, acct account.Account, req ResolvedSendRequest) SendResponse {
	s.mu.RLock()
	sender, ok := s.slack[acct.NameSlug()]
	s.mu.RUnlock()
	if !ok {
		return SendResponse{Error: fmt.Sprintf("no Slack workspace %q registered", acct.Display())}
	}

	// Choose API client based on identity.
	api := sender.BotAPI
	senderName := sender.BotName
	if req.Via == modelv1.ViaPigeonAsUser {
		api = sender.UserAPI
		senderName = sender.UserName
	}

	// For DMs, OpenConversation is deferred to send time (side effect).
	channelID := req.ResolvedSlack.ChannelID
	if req.ResolvedSlack.UserID != "" && channelID == "" {
		ch, _, _, err := api.OpenConversationContext(ctx, &goslack.OpenConversationParameters{
			Users: []string{req.ResolvedSlack.UserID},
		})
		if err != nil {
			return SendResponse{Error: fmt.Sprintf("open DM with %s (%s): %v", req.ResolvedSlack.Display(), req.ResolvedSlack.UserID, err)}
		}
		channelID = ch.ID
	}

	displayName := req.ResolvedSlack.Display()

	if req.DryRun {
		return SendResponse{
			OK:          true,
			ChannelID:   channelID,
			ChannelName: displayName,
			SendAs:      senderName,
		}
	}

	// Resolve @mentions in the resolved message text to Slack's <@USER_ID> format.
	message := sender.Resolver.ResolveMentions(req.ResolvedMessage)

	// Build message options.
	// Text is always set as the notification/fallback, even when blocks are
	// present. The message is mrkdwn-formatted; escape=false preserves it.
	opts := []goslack.MsgOption{goslack.MsgOptionText(message, false)}

	if req.Via == modelv1.ViaPigeonAsUser {
		// Wrap user-token messages in Block Kit so recipients can
		// distinguish automated sends from the human typing directly.
		opts = append(opts, goslack.MsgOptionBlocks(
			goslack.NewSectionBlock(
				goslack.NewTextBlockObject("mrkdwn", message, false, false),
				nil, nil,
			),
			goslack.NewContextBlock("",
				goslack.NewTextBlockObject("mrkdwn", "_sent via pigeon_", false, false),
			),
		))
	}

	// Attach metadata so the listener can identify pigeon-sent messages.
	opts = append(opts, goslack.MsgOptionMetadata(slacklistener.PigeonSendMetadata(req.Via)))

	if req.Thread != "" {
		opts = append(opts, goslack.MsgOptionTS(req.Thread))
		if req.Broadcast {
			opts = append(opts, goslack.MsgOptionBroadcast())
		}
	}

	// Schedule or send immediately.
	if req.PostAt != "" {
		_, scheduledID, err := api.ScheduleMessageContext(ctx, channelID, req.PostAt, opts...)
		if err != nil {
			return SendResponse{Error: fmt.Sprintf("schedule to %s failed: %v", displayName, err)}
		}
		return SendResponse{OK: true, ScheduledMessageID: scheduledID, SendAs: senderName}
	}

	// Send the message.
	_, ts, err := api.PostMessageContext(ctx, channelID, opts...)
	if err != nil {
		slog.ErrorContext(ctx, "slack send failed",
			"channel_id", channelID, "channel_name", displayName,
			"via", req.Via, "error", err)
		hint := ""
		if req.Via == modelv1.ViaPigeonAsBot {
			hint = slackChannelNotFoundHint(err)
		}
		return SendResponse{Error: fmt.Sprintf("send to %s failed: %v%s", displayName, err, hint)}
	}

	// The listener will pick up this message via Socket Mode and write it to
	// the store with the via field extracted from the pigeon_send metadata.
	msgTS := slacklistener.ParseTimestamp(ts)
	return SendResponse{OK: true, Timestamp: msgTS.Format(time.RFC3339), SendAs: senderName}
}

// StatusResponse is the daemon API response for GET /api/status.
type StatusResponse struct {
	Version                 string                     `json:"version"`
	PID                     int                        `json:"pid"`
	Executable              string                     `json:"executable"`
	StartedAt               time.Time                  `json:"started_at"`
	LogFile                 string                     `json:"log_file"`
	Listeners               map[string][]string        `json:"listeners"`
	SyncStatus              map[string]syncstatus.Info `json:"sync_status,omitempty"`
	ConnectedClaudeSessions []ClaudeSessionInfo        `json:"connected_claude_sessions"`
}

// ClaudeSessionInfo describes a connected Claude Code session in the status response.
type ClaudeSessionInfo struct {
	SessionID string `json:"session_id"`
	CWD       string `json:"cwd"`
	Account   string `json:"account"`
}

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	listeners := make(map[string][]string, 3)
	for slug := range s.slack {
		listeners["slack"] = append(listeners["slack"], slug)
	}
	for slug := range s.whatsapp {
		listeners["whatsapp"] = append(listeners["whatsapp"], slug)
	}
	for slug := range s.gws {
		listeners["gws"] = append(listeners["gws"], slug)
	}
	s.mu.RUnlock()

	sort.Strings(listeners["slack"])
	sort.Strings(listeners["whatsapp"])
	sort.Strings(listeners["gws"])

	connected := s.hub.ConnectedClaudeSessions()
	claudeSessions := make([]ClaudeSessionInfo, len(connected))
	for i, cs := range connected {
		claudeSessions[i] = ClaudeSessionInfo{
			SessionID: cs.SessionID,
			CWD:       cs.CWD,
			Account:   cs.Account,
		}
	}

	exePath, err := os.Executable()
	if err != nil {
		slog.Error("resolve executable path", "error", err)
	}

	writeJSON(w, http.StatusOK, StatusResponse{
		Version:                 s.version,
		PID:                     os.Getpid(),
		Executable:              exePath,
		StartedAt:               s.startedAt,
		LogFile:                 paths.DaemonLogPath(),
		Listeners:               listeners,
		SyncStatus:              s.syncTracker.All(),
		ConnectedClaudeSessions: claudeSessions,
	})
}

func (s *Server) handleSessionConnected(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		http.Error(w, "session_id query param required", http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{
		"connected": s.hub.SessionConnected(sessionID),
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// formatAmbiguousContacts builds a disambiguation message enriched with
// conversation activity (last message date, total messages) from the file store.
func formatAmbiguousContacts(err *walistener.AmbiguousContactError, acct account.Account) string {
	var b strings.Builder
	fmt.Fprintf(&b, "multiple contacts match %q:\n", err.Query)

	for _, m := range err.Matches {
		convDir := m.Phone // conversation directories are "+phone"
		lastDate, msgCount := convActivity(acct, convDir)

		fmt.Fprintf(&b, "  %s  %s", m.Phone, m.Name)
		if msgCount > 0 {
			fmt.Fprintf(&b, "  (last msg: %s, %d messages)", lastDate, msgCount)
		} else {
			b.WriteString("  (no conversation history)")
		}
		b.WriteString("\n")
	}
	b.WriteString("use a phone number or full name to disambiguate")
	return b.String()
}

// convActivity returns the most recent message date and total line count
// for a conversation directory.
func convActivity(acct account.Account, conversation string) (lastDate string, totalLines int) {
	dir := paths.DefaultDataRoot().AccountFor(acct).Conversation(conversation).Path()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", 0
	}

	var dates []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), paths.FileExt) {
			dates = append(dates, strings.TrimSuffix(e.Name(), paths.FileExt))
		}
	}
	if len(dates) == 0 {
		return "", 0
	}
	sort.Strings(dates)

	// Count lines across all files.
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), paths.FileExt) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			if line != "" {
				totalLines++
			}
		}
	}

	return dates[len(dates)-1], totalLines
}

// checkMPDMBotAccess rejects bot-token sends to group DMs where the bot is
// not a member. Bots cannot access MPDMs they haven't been invited to, so we
// fail early before the message enters the outbox.
func (s *Server) checkMPDMBotAccess(ctx context.Context, req SendRequest) error {
	if req.Platform != "slack" || req.Slack == nil {
		return nil
	}
	if !strings.HasPrefix(req.Slack.Channel, "@mpdm-") {
		return nil
	}
	if req.Via == modelv1.ViaPigeonAsUser {
		return nil
	}

	acct := account.New(req.Platform, req.Account)
	s.mu.RLock()
	sender, ok := s.slack[acct.NameSlug()]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("no Slack workspace %q registered", acct.Display())
	}

	channelID, _, err := sender.Resolver.FindChannelID(ctx, req.Slack.Channel)
	if err != nil {
		return fmt.Errorf("resolve MPDM channel: %w", err)
	}
	if !sender.Resolver.IsBotMember(channelID) {
		return fmt.Errorf("bot is not a member of this group DM — use --via pigeon-as-user to send as yourself")
	}
	return nil
}

// resolveSlackDisplay resolves a SlackTarget to a ResolvedSlackTarget with
// display names populated but without side effects (no OpenConversation).
// Used at submit time so the outbox has human-readable names for the TUI.
func resolveSlackDisplay(ctx context.Context, sender *SlackSender, t *SlackTarget) (*ResolvedSlackTarget, error) {
	switch {
	case t.UserID != "":
		userName, err := sender.Resolver.UserName(ctx, t.UserID)
		if err != nil {
			return nil, fmt.Errorf("unknown user %s: %v", t.UserID, err)
		}
		return &ResolvedSlackTarget{
			UserID:   t.UserID,
			UserName: userName,
		}, nil

	case t.Channel != "":
		channelID, channelName, err := sender.Resolver.FindChannelID(ctx, t.Channel)
		if err != nil {
			return nil, err
		}
		return &ResolvedSlackTarget{
			ChannelID:   channelID,
			ChannelName: channelName,
		}, nil
	}
	return nil, fmt.Errorf("empty slack target")
}

// resolveSlackTarget fully resolves a SlackTarget, including opening DM
// channels. Used by the send/react/delete paths that need routing info.
func resolveSlackTarget(ctx context.Context, sender *SlackSender, api *goslack.Client, t *SlackTarget) (*ResolvedSlackTarget, error) {
	resolved, err := resolveSlackDisplay(ctx, sender, t)
	if err != nil {
		return nil, err
	}
	// For DMs, open the conversation to get the channel ID.
	if resolved.UserID != "" && resolved.ChannelID == "" {
		ch, _, _, err := api.OpenConversationContext(ctx, &goslack.OpenConversationParameters{
			Users: []string{resolved.UserID},
		})
		if err != nil {
			return nil, fmt.Errorf("open DM with %s (%s): %v", resolved.Display(), resolved.UserID, err)
		}
		resolved.ChannelID = ch.ID
	}
	return resolved, nil
}

func (s *Server) dispatchSend(ctx context.Context, resolved ResolvedSendRequest) SendResponse {
	acct := account.New(resolved.Platform, resolved.Account)
	switch acct.Platform {
	case "whatsapp":
		return s.sendWhatsApp(ctx, acct, resolved.SendRequest)
	case "slack":
		return s.sendSlack(ctx, acct, resolved)
	default:
		return SendResponse{Error: fmt.Sprintf("unsupported platform: %s", resolved.Platform)}
	}
}

// executeSend is the outbox.SendFunc callback. It unmarshals the stored payload
// and dispatches through the normal send path.
func (s *Server) executeSend(ctx context.Context, payload json.RawMessage) (bool, string) {
	var resolved ResolvedSendRequest
	if err := json.Unmarshal(payload, &resolved); err != nil {
		return false, "invalid payload: " + err.Error()
	}
	resp := s.dispatchSend(ctx, resolved)
	if !resp.OK {
		return false, resp.Error
	}
	return true, ""
}

// ReactRequest is the daemon API payload for /api/react.
type ReactRequest struct {
	Platform string `json:"platform"`
	Account  string `json:"account"`

	// Target — platform-specific, exactly one must be set.
	Slack   *SlackTarget `json:"slack,omitempty"`
	Contact string       `json:"contact,omitempty"` // WhatsApp contact name or phone

	MessageID string `json:"message_id"`
	Emoji     string `json:"emoji"`
	Remove    bool   `json:"remove,omitempty"`
}

// ReactResponse is the daemon API response for /api/react.
type ReactResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func (s *Server) handleReact(w http.ResponseWriter, r *http.Request) {
	var req ReactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ReactResponse{Error: "invalid JSON: " + err.Error()})
		return
	}

	if req.Platform == "" || req.Account == "" || req.MessageID == "" || req.Emoji == "" {
		writeJSON(w, http.StatusBadRequest, ReactResponse{Error: "platform, account, message_id, and emoji are required"})
		return
	}
	if err := validateTarget(req.Platform, req.Slack, req.Contact); err != nil {
		writeJSON(w, http.StatusBadRequest, ReactResponse{Error: err.Error()})
		return
	}

	resp := s.dispatchReact(r.Context(), req)
	status := http.StatusOK
	if !resp.OK {
		status = http.StatusInternalServerError
	}
	writeJSON(w, status, resp)
}

// dispatchReact routes a ReactRequest to the appropriate platform.
func (s *Server) dispatchReact(ctx context.Context, req ReactRequest) ReactResponse {
	acct := account.New(req.Platform, req.Account)
	switch acct.Platform {
	case "slack":
		return s.reactSlack(ctx, acct, req)
	case "whatsapp":
		return s.reactWhatsApp(ctx, acct, req)
	default:
		return ReactResponse{Error: fmt.Sprintf("unsupported platform: %s", req.Platform)}
	}
}

func (s *Server) reactSlack(ctx context.Context, acct account.Account, req ReactRequest) ReactResponse {
	s.mu.RLock()
	sender, ok := s.slack[acct.NameSlug()]
	s.mu.RUnlock()
	if !ok {
		return ReactResponse{Error: fmt.Sprintf("no Slack workspace %q registered", acct.Display())}
	}

	resolved, err := resolveSlackTarget(ctx, sender, sender.BotAPI, req.Slack)
	if err != nil {
		return ReactResponse{Error: err.Error()}
	}

	channelID := resolved.ChannelID
	displayName := resolved.Display()

	// Reactions are always sent via the bot token.
	ref := goslack.NewRefToMessage(channelID, req.MessageID)
	var reactErr error
	if req.Remove {
		reactErr = sender.BotAPI.RemoveReactionContext(ctx, req.Emoji, ref)
	} else {
		reactErr = sender.BotAPI.AddReactionContext(ctx, req.Emoji, ref)
	}
	if reactErr != nil {
		return ReactResponse{Error: fmt.Sprintf("react on %s: %v%s", displayName, reactErr, slackChannelNotFoundHint(reactErr))}
	}

	// Store locally. Derive date file from the message timestamp.
	msgTS := slacklistener.ParseTimestamp(req.MessageID)
	lineType := modelv1.LineReaction
	if req.Remove {
		lineType = modelv1.LineUnreaction
	}

	senderName := sender.BotName
	senderID := sender.BotUserID

	// Use the message timestamp for file placement: the protocol requires
	// reactions to land in the same date file as the target message.
	// store.Append derives the date file from line.Ts(), so we use msgTS.
	line := modelv1.Line{
		Type: lineType,
		React: &modelv1.ReactLine{
			Ts:       msgTS,
			MsgID:    req.MessageID,
			Sender:   senderName,
			SenderID: senderID,
			Via:      modelv1.ViaPigeonAsBot,
			Emoji:    req.Emoji,
			Remove:   req.Remove,
		},
	}
	if err := s.store.Append(sender.Acct, displayName, line); err != nil {
		slog.ErrorContext(ctx, "failed to store reaction", "error", err)
	}

	// Also append to thread file if one exists for this message.
	if s.store.ThreadExists(sender.Acct, displayName, req.MessageID) {
		if err := s.store.AppendThread(sender.Acct, displayName, req.MessageID, line); err != nil {
			slog.ErrorContext(ctx, "failed to store reaction in thread", "error", err)
		}
	}

	return ReactResponse{OK: true}
}

func (s *Server) reactWhatsApp(ctx context.Context, acct account.Account, req ReactRequest) ReactResponse {
	s.mu.RLock()
	sender, ok := s.whatsapp[acct.NameSlug()]
	s.mu.RUnlock()
	if !ok {
		return ReactResponse{Error: fmt.Sprintf("no WhatsApp account %q registered", acct.Display())}
	}

	// Resolve contact query to JID.
	recipientJID, err := sender.Resolver.FindJID(ctx, req.Contact)
	if err != nil {
		var ambErr *walistener.AmbiguousContactError
		if errors.As(err, &ambErr) {
			return ReactResponse{Error: formatAmbiguousContacts(ambErr, sender.Acct)}
		}
		return ReactResponse{Error: fmt.Sprintf("resolve contact: %v", err)}
	}

	// Build and send the reaction message.
	// For unreact, WhatsApp uses an empty string as the reaction text.
	emoji := req.Emoji
	if req.Remove {
		emoji = ""
	}

	var senderJID types.JID
	if sender.Client.Store.ID != nil {
		senderJID = types.NewJID(sender.Client.Store.ID.User, types.DefaultUserServer)
	}

	reactionMsg := sender.Client.BuildReaction(recipientJID, senderJID, req.MessageID, emoji)
	_, err = sender.Client.SendMessage(ctx, recipientJID, reactionMsg)
	if err != nil {
		return ReactResponse{Error: fmt.Sprintf("send reaction: %v", err)}
	}

	// Store locally.
	convDir := sender.Resolver.ConvDir(ctx, recipientJID)
	senderName := "me"
	var senderID string
	if !senderJID.IsEmpty() {
		senderName = sender.Resolver.ContactName(ctx, senderJID)
		senderID = senderJID.String()
	}

	lineType := modelv1.LineReaction
	if req.Remove {
		lineType = modelv1.LineUnreaction
	}
	line := modelv1.Line{
		Type: lineType,
		React: &modelv1.ReactLine{
			Ts:       time.Now().UTC(),
			MsgID:    req.MessageID,
			Sender:   senderName,
			SenderID: senderID,
			Via:      modelv1.ViaPigeonAsBot,
			Emoji:    req.Emoji,
			Remove:   req.Remove,
		},
	}
	if err := s.store.Append(sender.Acct, convDir, line); err != nil {
		slog.ErrorContext(ctx, "failed to store reaction", "error", err)
	}

	return ReactResponse{OK: true}
}

// DeleteRequest is the daemon API request for /api/delete.
type DeleteRequest struct {
	Platform string `json:"platform"`
	Account  string `json:"account"`

	// Target — platform-specific.
	Slack *SlackTarget `json:"slack,omitempty"`

	MessageID string `json:"message_id"`
}

// DeleteResponse is the daemon API response for /api/delete.
type DeleteResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func (s *Server) handleDeleteMsg(w http.ResponseWriter, r *http.Request) {
	var req DeleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, DeleteResponse{Error: "invalid JSON: " + err.Error()})
		return
	}

	if req.Platform == "" || req.Account == "" || req.MessageID == "" {
		writeJSON(w, http.StatusBadRequest, DeleteResponse{Error: "platform, account, and message_id are required"})
		return
	}
	if req.Platform != "slack" {
		writeJSON(w, http.StatusBadRequest, DeleteResponse{Error: "delete is only supported for Slack"})
		return
	}
	if req.Slack == nil {
		writeJSON(w, http.StatusBadRequest, DeleteResponse{Error: "slack target (user_id or channel) is required"})
		return
	}
	if err := req.Slack.Validate(); err != nil {
		writeJSON(w, http.StatusBadRequest, DeleteResponse{Error: err.Error()})
		return
	}

	resp := s.deleteSlack(r.Context(), account.New(req.Platform, req.Account), req)
	status := http.StatusOK
	if !resp.OK {
		status = http.StatusInternalServerError
	}
	writeJSON(w, status, resp)
}

func (s *Server) deleteSlack(ctx context.Context, acct account.Account, req DeleteRequest) DeleteResponse {
	s.mu.RLock()
	sender, ok := s.slack[acct.NameSlug()]
	s.mu.RUnlock()
	if !ok {
		return DeleteResponse{Error: fmt.Sprintf("no Slack workspace %q registered", acct.Display())}
	}

	resolved, err := resolveSlackTarget(ctx, sender, sender.BotAPI, req.Slack)
	if err != nil {
		return DeleteResponse{Error: err.Error()}
	}

	channelID := resolved.ChannelID
	displayName := resolved.Display()

	// Bots can only delete their own messages via chat.delete.
	// The delete event will come back through the websocket and the listener
	// stores it locally — no need to duplicate that here.
	_, _, err = sender.BotAPI.DeleteMessageContext(ctx, channelID, req.MessageID)
	if err != nil {
		return DeleteResponse{Error: fmt.Sprintf("delete in %s: %v%s", displayName, err, slackChannelNotFoundHint(err))}
	}

	slog.InfoContext(ctx, "slack message deleted",
		"msg_id", req.MessageID, "channel", displayName, "account", acct)

	return DeleteResponse{OK: true}
}
