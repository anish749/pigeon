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
	walistener "github.com/anish749/pigeon/internal/listener/whatsapp"
	"github.com/anish749/pigeon/internal/outbox"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
	"github.com/anish749/pigeon/internal/store/modelv1"

	slacklistener "github.com/anish749/pigeon/internal/listener/slack"
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
	mu        sync.RWMutex
	whatsapp  map[string]*WhatsAppSender // account slug → sender
	slack     map[string]*SlackSender    // account slug → sender
	hub       *hub.Hub
	outbox    *outbox.Outbox
	store     store.Store
	version   string
	startedAt time.Time
}

// NewServer creates a new API server.
func NewServer(h *hub.Hub, ob *outbox.Outbox, s store.Store, version string) *Server {
	return &Server{
		whatsapp:  make(map[string]*WhatsAppSender),
		slack:     make(map[string]*SlackSender),
		hub:       h,
		outbox:    ob,
		store:     s,
		version:   version,
		startedAt: time.Now(),
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
	Platform  string `json:"platform"`
	Account   string `json:"account"`
	Contact   string `json:"contact"`
	Message   string `json:"message"`
	Thread    string `json:"thread,omitempty"`
	Broadcast bool   `json:"broadcast,omitempty"`
	AsUser    bool   `json:"as_user,omitempty"`
	DryRun    bool   `json:"dry_run,omitempty"`
	// SessionID, when set, routes the send through the outbox for human
	// review instead of sending immediately. Set automatically by the CLI
	// when PIGEON_SESSION_ID is in the environment.
	SessionID string `json:"session_id,omitempty"`
}

// SendResponse is the daemon API response for /api/send.
type SendResponse struct {
	OK          bool   `json:"ok"`
	Timestamp   string `json:"timestamp,omitempty"`
	Error       string `json:"error,omitempty"`
	ChannelID   string `json:"channel_id,omitempty"`
	ChannelName string `json:"channel_name,omitempty"`
	SendAs      string `json:"send_as,omitempty"`
	Email       string `json:"email,omitempty"`
	OutboxID    string `json:"outbox_id,omitempty"`
}

func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	var req SendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, SendResponse{Error: "invalid JSON: " + err.Error()})
		return
	}

	if req.Platform == "" || req.Account == "" || req.Contact == "" || req.Message == "" {
		writeJSON(w, http.StatusBadRequest, SendResponse{Error: "platform, account, contact, and message are required"})
		return
	}

	// When a session ID is present, queue for review instead of sending.
	if req.SessionID != "" {
		payload, err := json.Marshal(req)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, SendResponse{Error: "marshal send request: " + err.Error()})
			return
		}
		item := s.outbox.Submit(req.SessionID, payload)
		slog.Info("outbox item submitted", "id", item.ID, "session_id", req.SessionID)
		writeJSON(w, http.StatusOK, SendResponse{OK: true, OutboxID: item.ID})
		return
	}

	resp := s.dispatchSend(r.Context(), req)
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

	// Store locally.
	convDir := sender.Resolver.ConvDir(ctx, recipientJID)
	senderName := "me"
	var senderID string
	if sender.Client.Store.ID != nil {
		myJID := types.NewJID(sender.Client.Store.ID.User, types.DefaultUserServer)
		senderName = sender.Resolver.ContactName(ctx, myJID)
		senderID = myJID.String()
	}
	via := modelv1.ViaPigeonAsUser
	if !req.AsUser {
		via = modelv1.ViaPigeonAsBot
	}
	line := modelv1.Line{
		Type: modelv1.LineMessage,
		Msg: &modelv1.MsgLine{
			ID:       resp.ID,
			Ts:       resp.Timestamp,
			Sender:   senderName,
			SenderID: senderID,
			Via:      via,
			Text:     req.Message,
		},
	}
	if err := s.store.Append(sender.Acct, convDir, line); err != nil {
		slog.ErrorContext(ctx, "failed to store sent message", "error", err)
	}

	// Write .meta.json for the conversation.
	meta := store.ConversationMeta{
		Name: sender.Resolver.ContactName(ctx, recipientJID),
		Type: "dm",
		JID:  recipientJID.String(),
	}
	if recipientJID.Server == types.GroupServer {
		meta.Type = "group"
		meta.Name = sender.Resolver.GroupName(ctx, recipientJID)
	}
	if err := s.store.WriteMeta(sender.Acct, convDir, meta); err != nil {
		slog.WarnContext(ctx, "failed to write .meta.json", "conv", convDir, "error", err)
	}

	return SendResponse{OK: true, Timestamp: resp.Timestamp.Format(time.RFC3339)}
}

func (s *Server) sendSlack(ctx context.Context, acct account.Account, req SendRequest) SendResponse {
	s.mu.RLock()
	sender, ok := s.slack[acct.NameSlug()]
	s.mu.RUnlock()
	if !ok {
		return SendResponse{Error: fmt.Sprintf("no Slack workspace %q registered", acct.Display())}
	}

	// Choose API client based on identity.
	api := sender.BotAPI
	senderName := sender.BotName
	if req.AsUser {
		api = sender.UserAPI
		senderName = sender.UserName
	}

	// Resolve contact to a channel ID.
	// Try as user ID, then email, then channel name.
	var channelID, channelName string
	var resolvedUserID string

	if userID, userName, err := sender.Resolver.FindUserID(req.Contact); err == nil && userID == req.Contact {
		resolvedUserID = userID
		channelName = "@" + userName
	} else if looksLikeEmail(req.Contact) {
		if user, err := sender.UserAPI.GetUserByEmailContext(ctx, req.Contact); err == nil {
			resolvedUserID = user.ID
			name := user.Profile.DisplayName
			if name == "" {
				name = user.RealName
			}
			channelName = "@" + name
		}
	}

	if resolvedUserID != "" {
		// Open DM with the appropriate token.
		ch, _, _, openErr := api.OpenConversationContext(ctx, &goslack.OpenConversationParameters{
			Users: []string{resolvedUserID},
		})
		if openErr != nil {
			return SendResponse{Error: fmt.Sprintf(
				"open DM with %s (%s) failed: %v — for Slack Connect users, the bot must be a member of at least one shared channel with the recipient",
				channelName, resolvedUserID, openErr)}
		}
		channelID = ch.ID
		if !req.AsUser {
			senderName = "sent by pigeon"
		}
	} else {
		var err error
		channelID, channelName, err = sender.Resolver.FindChannelID(ctx, req.Contact)
		if err != nil {
			return SendResponse{Error: fmt.Sprintf("resolve channel: %v", err)}
		}
	}

	// For bot sends to DMs/group DMs resolved via channel name (not user ID/email),
	// the cached channel ID is from the user token and isn't accessible to the bot.
	// Open the bot's own conversation.
	if resolvedUserID == "" && !req.AsUser && strings.HasPrefix(channelName, "@") {
		var userIDs []string

		if strings.HasPrefix(channelName, "@mpdm-") {
			// Group DM: look up members via user token, then open with bot token.
			members, _, err := sender.UserAPI.GetUsersInConversationContext(ctx, &goslack.GetUsersInConversationParameters{
				ChannelID: channelID,
			})
			if err != nil {
				return SendResponse{Error: fmt.Sprintf("get members of %s: %v", channelName, err)}
			}
			userIDs = members
		} else {
			// 1:1 DM: find the target user ID.
			userID, _, userErr := sender.Resolver.FindUserID(channelName)
			if userErr != nil {
				var ambErr *slacklistener.AmbiguousUserError
				if errors.As(userErr, &ambErr) {
					return SendResponse{Error: formatAmbiguousUsers(ctx, ambErr, sender)}
				}
				return SendResponse{Error: fmt.Sprintf("resolve user %s: %v", channelName, userErr)}
			}
			userIDs = []string{userID}
		}

		ch, _, _, openErr := api.OpenConversationContext(ctx, &goslack.OpenConversationParameters{
			Users: userIDs,
		})
		if openErr != nil {
			return SendResponse{Error: fmt.Sprintf("open conversation with %s: %v", channelName, openErr)}
		}
		channelID = ch.ID
		senderName = "sent by pigeon"
	}

	if req.DryRun {
		resp := SendResponse{
			OK:          true,
			ChannelID:   channelID,
			ChannelName: channelName,
			SendAs:      senderName,
		}
		// For DMs, enrich with user email.
		if strings.HasPrefix(channelName, "@") && !strings.HasPrefix(channelName, "@mpdm-") {
			if userID, _, err := sender.Resolver.FindUserID(channelName); err == nil {
				if user, err := sender.UserAPI.GetUserInfoContext(ctx, userID); err == nil && user.Profile.Email != "" {
					resp.Email = user.Profile.Email
				}
			}
		}
		return resp
	}

	// Build message options.
	opts := []goslack.MsgOption{goslack.MsgOptionText(req.Message, false)}
	if req.Thread != "" {
		opts = append(opts, goslack.MsgOptionTS(req.Thread))
		if req.Broadcast {
			opts = append(opts, goslack.MsgOptionBroadcast())
		}
	}

	// Send the message.
	_, ts, err := api.PostMessageContext(ctx, channelID, opts...)
	if err != nil {
		slog.ErrorContext(ctx, "slack send failed",
			"channel_id", channelID, "channel_name", channelName,
			"as_user", req.AsUser, "error", err)
		if err.Error() == "channel_not_found" && !req.AsUser {
			return SendResponse{Error: fmt.Sprintf(
				"send to %s failed: %v — bot cannot access this channel. For Slack Connect users, ensure the bot is a member of at least one shared channel with the recipient. For private channels, ask the user to invite the bot to %s.",
				channelName, err, channelName)}
		}
		return SendResponse{Error: fmt.Sprintf("send to %s failed: %v", channelName, err)}
	}

	// Store locally.
	msgTS := slacklistener.ParseTimestamp(ts)
	via := modelv1.ViaPigeonAsBot
	if req.AsUser {
		via = modelv1.ViaPigeonAsUser
	}
	senderID := sender.BotUserID
	if req.AsUser {
		senderID = sender.UserID
	}
	line := modelv1.Line{
		Type: modelv1.LineMessage,
		Msg: &modelv1.MsgLine{
			ID:       ts,
			Ts:       msgTS,
			Sender:   senderName,
			SenderID: senderID,
			Via:      via,
			Text:     req.Message,
		},
	}
	if req.Thread != "" {
		line.Msg.Reply = true
		if err := s.store.AppendThread(sender.Acct, channelName, req.Thread, line); err != nil {
			slog.ErrorContext(ctx, "failed to store sent thread message", "error", err)
		}
	} else {
		if err := s.store.Append(sender.Acct, channelName, line); err != nil {
			slog.ErrorContext(ctx, "failed to store sent message", "error", err)
		}
	}

	// Write .meta.json for the conversation.
	meta := store.ConversationMeta{
		Name:      channelName,
		ChannelID: channelID,
	}
	switch {
	case strings.HasPrefix(channelName, "@mpdm-"):
		meta.Type = "group_dm"
	case strings.HasPrefix(channelName, "@"):
		meta.Type = "dm"
		meta.UserID = sender.Resolver.DMUserID(channelName)
	default:
		meta.Type = "channel"
	}
	if err := s.store.WriteMeta(sender.Acct, channelName, meta); err != nil {
		slog.WarnContext(ctx, "failed to write .meta.json", "channel", channelName, "error", err)
	}

	return SendResponse{OK: true, Timestamp: msgTS.Format(time.RFC3339)}
}

// StatusResponse is the daemon API response for GET /api/status.
type StatusResponse struct {
	Version                 string              `json:"version"`
	PID                     int                 `json:"pid"`
	StartedAt               time.Time           `json:"started_at"`
	LogFile                 string              `json:"log_file"`
	Listeners               map[string][]string `json:"listeners"`
	ConnectedClaudeSessions []ClaudeSessionInfo `json:"connected_claude_sessions"`
}

// ClaudeSessionInfo describes a connected Claude Code session in the status response.
type ClaudeSessionInfo struct {
	SessionID string `json:"session_id"`
	CWD       string `json:"cwd"`
	Account   string `json:"account"`
}

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	listeners := make(map[string][]string, 2)
	for slug := range s.slack {
		listeners["slack"] = append(listeners["slack"], slug)
	}
	for slug := range s.whatsapp {
		listeners["whatsapp"] = append(listeners["whatsapp"], slug)
	}
	s.mu.RUnlock()

	sort.Strings(listeners["slack"])
	sort.Strings(listeners["whatsapp"])

	connected := s.hub.ConnectedClaudeSessions()
	claudeSessions := make([]ClaudeSessionInfo, len(connected))
	for i, cs := range connected {
		claudeSessions[i] = ClaudeSessionInfo{
			SessionID: cs.SessionID,
			CWD:       cs.CWD,
			Account:   cs.Account,
		}
	}

	writeJSON(w, http.StatusOK, StatusResponse{
		Version:                 s.version,
		PID:                     os.Getpid(),
		StartedAt:               s.startedAt,
		LogFile:                 paths.DaemonLogPath(),
		Listeners:               listeners,
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

// formatAmbiguousUsers builds a disambiguation message for Slack users,
// enriched with conversation activity from the Slack API.
func formatAmbiguousUsers(ctx context.Context, err *slacklistener.AmbiguousUserError, sender *SlackSender) string {
	var b strings.Builder
	fmt.Fprintf(&b, "multiple users match %q:\n", err.Query)
	for _, m := range err.Matches {
		fmt.Fprintf(&b, "  %s  %s", m.ID, m.DisplayName)
		if m.RealName != "" && m.RealName != m.DisplayName {
			fmt.Fprintf(&b, "  (%s)", m.RealName)
		}
		if m.Email != "" {
			fmt.Fprintf(&b, "  <%s>", m.Email)
		}

		// Check actual Slack conversation history for this specific user ID.
		ch, _, _, openErr := sender.UserAPI.OpenConversationContext(ctx, &goslack.OpenConversationParameters{
			Users: []string{m.ID},
		})
		if openErr != nil {
			fmt.Fprintf(&b, "  [cannot open DM: %v]", openErr)
		} else {
			hist, histErr := sender.UserAPI.GetConversationHistoryContext(ctx, &goslack.GetConversationHistoryParameters{
				ChannelID: ch.ID,
				Limit:     1,
			})
			if histErr != nil {
				fmt.Fprintf(&b, "  [cannot read history: %v]", histErr)
			} else if len(hist.Messages) > 0 {
				lastTS := slacklistener.ParseTimestamp(hist.Messages[0].Timestamp)
				fmt.Fprintf(&b, "  [last msg: %s]", lastTS.Format("2006-01-02"))
			} else {
				b.WriteString("  [no conversation history]")
			}
		}

		b.WriteString("\n")
	}
	b.WriteString("ask the user to confirm which person to send to, then use their user ID")
	return b.String()
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

// dispatchSend routes a SendRequest to the appropriate platform sender.
func (s *Server) dispatchSend(ctx context.Context, req SendRequest) SendResponse {
	acct := account.New(req.Platform, req.Account)
	switch acct.Platform {
	case "whatsapp":
		return s.sendWhatsApp(ctx, acct, req)
	case "slack":
		return s.sendSlack(ctx, acct, req)
	default:
		return SendResponse{Error: fmt.Sprintf("unsupported platform: %s", req.Platform)}
	}
}

// executeSend is the outbox.SendFunc callback. It unmarshals the stored payload
// and dispatches through the normal send path.
func (s *Server) executeSend(ctx context.Context, payload json.RawMessage) (bool, string) {
	var req SendRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return false, "invalid payload: " + err.Error()
	}
	resp := s.dispatchSend(ctx, req)
	if !resp.OK {
		return false, resp.Error
	}
	return true, ""
}

// ReactRequest is the daemon API payload for /api/react.
type ReactRequest struct {
	Platform  string `json:"platform"`
	Account   string `json:"account"`
	Contact   string `json:"contact"`
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

	if req.Platform == "" || req.Account == "" || req.Contact == "" || req.MessageID == "" || req.Emoji == "" {
		writeJSON(w, http.StatusBadRequest, ReactResponse{Error: "platform, account, contact, message_id, and emoji are required"})
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

	// Resolve contact to a channel ID.
	channelID, channelName, err := sender.Resolver.FindChannelID(ctx, req.Contact)
	if err != nil {
		return ReactResponse{Error: fmt.Sprintf("resolve channel: %v", err)}
	}

	// Reactions are always sent via the bot token.
	ref := goslack.NewRefToMessage(channelID, req.MessageID)
	if req.Remove {
		err = sender.BotAPI.RemoveReactionContext(ctx, req.Emoji, ref)
	} else {
		err = sender.BotAPI.AddReactionContext(ctx, req.Emoji, ref)
	}
	if err != nil {
		return ReactResponse{Error: fmt.Sprintf("react on %s: %v", channelName, err)}
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
	if err := s.store.Append(sender.Acct, channelName, line); err != nil {
		slog.ErrorContext(ctx, "failed to store reaction", "error", err)
	}

	// Also append to thread file if one exists for this message.
	if s.store.ThreadExists(sender.Acct, channelName, req.MessageID) {
		if err := s.store.AppendThread(sender.Acct, channelName, req.MessageID, line); err != nil {
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
