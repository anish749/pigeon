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

	walistener "github.com/anish/claude-msg-utils/internal/listener/whatsapp"
	"github.com/anish/claude-msg-utils/internal/store"

	slacklistener "github.com/anish/claude-msg-utils/internal/listener/slack"
)

// Port is the daemon API's listen port.
const Port = 9877

// WhatsAppSender holds everything needed to send a WhatsApp message.
type WhatsAppSender struct {
	Client   *whatsmeow.Client
	Account  string
	Resolver *walistener.Resolver
}

// SlackSender holds everything needed to send a Slack message.
type SlackSender struct {
	BotAPI    *goslack.Client // bot token client (default for sends)
	UserAPI   *goslack.Client // user token client (--as-user sends)
	Resolver  *slacklistener.Resolver
	Messages  *slacklistener.MessageStore
	Workspace string
	BotName   string // the bot's display name
	UserName  string // the authenticated user's display name
}

// Server is the daemon's HTTP API server.
type Server struct {
	mu       sync.RWMutex
	whatsapp map[string]*WhatsAppSender // account → sender
	slack    map[string]*SlackSender    // workspace → sender
}

// NewServer creates a new API server.
func NewServer() *Server {
	return &Server{
		whatsapp: make(map[string]*WhatsAppSender),
		slack:    make(map[string]*SlackSender),
	}
}

// RegisterWhatsApp registers a WhatsApp client for sending.
func (s *Server) RegisterWhatsApp(sender *WhatsAppSender) {
	s.mu.Lock()
	s.whatsapp[sender.Account] = sender
	s.mu.Unlock()
}

// RegisterSlack registers a Slack client for sending.
func (s *Server) RegisterSlack(sender *SlackSender) {
	s.mu.Lock()
	s.slack[sender.Workspace] = sender
	s.mu.Unlock()
}

// Start starts the HTTP server. Blocks until ctx is cancelled.
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/send", s.handleSend)

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", Port),
		Handler: mux,
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
	}

	go func() {
		<-ctx.Done()
		srv.Close()
	}()

	slog.InfoContext(ctx, "api server started", "port", Port)
	err := srv.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

type sendRequest struct {
	Platform  string `json:"platform"`
	Account   string `json:"account"`
	Contact   string `json:"contact"`
	Message   string `json:"message"`
	Thread    string `json:"thread,omitempty"`
	Broadcast bool   `json:"broadcast,omitempty"`
	AsUser    bool   `json:"as_user,omitempty"`
}

type sendResponse struct {
	OK        bool   `json:"ok"`
	Timestamp string `json:"timestamp,omitempty"`
	Error     string `json:"error,omitempty"`
}

func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	var req sendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, sendResponse{Error: "invalid JSON: " + err.Error()})
		return
	}

	if req.Platform == "" || req.Account == "" || req.Contact == "" || req.Message == "" {
		writeJSON(w, http.StatusBadRequest, sendResponse{Error: "platform, account, contact, and message are required"})
		return
	}

	ctx := r.Context()
	var resp sendResponse

	switch req.Platform {
	case "whatsapp":
		resp = s.sendWhatsApp(ctx, req)
	case "slack":
		resp = s.sendSlack(ctx, req)
	default:
		resp = sendResponse{Error: fmt.Sprintf("unsupported platform: %s", req.Platform)}
	}

	status := http.StatusOK
	if !resp.OK {
		status = http.StatusInternalServerError
	}
	writeJSON(w, status, resp)
}

func (s *Server) sendWhatsApp(ctx context.Context, req sendRequest) sendResponse {
	s.mu.RLock()
	sender, ok := s.whatsapp[req.Account]
	s.mu.RUnlock()
	if !ok {
		return sendResponse{Error: fmt.Sprintf("no WhatsApp account %q registered", req.Account)}
	}

	// Resolve contact query to JID.
	recipientJID, err := sender.Resolver.FindJID(ctx, req.Contact)
	if err != nil {
		var ambErr *walistener.AmbiguousContactError
		if errors.As(err, &ambErr) {
			return sendResponse{Error: formatAmbiguousContacts(ambErr, sender.Account)}
		}
		return sendResponse{Error: fmt.Sprintf("resolve contact: %v", err)}
	}

	// Send the message.
	resp, err := sender.Client.SendMessage(ctx, recipientJID, &waE2E.Message{
		Conversation: proto.String(req.Message),
	})
	if err != nil {
		return sendResponse{Error: fmt.Sprintf("send: %v", err)}
	}

	// Store locally.
	convDir := sender.Resolver.ConvDir(ctx, recipientJID)
	senderName := "me"
	if sender.Client.Store.ID != nil {
		senderName = sender.Resolver.ContactName(ctx, types.NewJID(sender.Client.Store.ID.User, types.DefaultUserServer))
	}
	if err := store.WriteMessage("whatsapp", sender.Account, convDir, senderName, req.Message, resp.Timestamp); err != nil {
		slog.ErrorContext(ctx, "failed to store sent message", "error", err)
	}

	return sendResponse{OK: true, Timestamp: resp.Timestamp.Format(time.RFC3339)}
}

func (s *Server) sendSlack(ctx context.Context, req sendRequest) sendResponse {
	s.mu.RLock()
	sender, ok := s.slack[req.Account]
	s.mu.RUnlock()
	if !ok {
		return sendResponse{Error: fmt.Sprintf("no Slack workspace %q registered", req.Account)}
	}

	// Choose API client based on identity.
	api := sender.BotAPI
	senderName := sender.BotName
	if req.AsUser {
		api = sender.UserAPI
		senderName = sender.UserName
	}

	// Resolve contact/channel query to channel ID.
	channelID, channelName, err := sender.Resolver.FindChannelID(req.Contact)
	if err != nil {
		// If no cached channel matched an @-prefixed query, try opening a DM.
		if strings.HasPrefix(req.Contact, "@") || !strings.HasPrefix(req.Contact, "#") {
			if userID, userName, userErr := sender.Resolver.FindUserID(req.Contact); userErr == nil {
				ch, _, _, openErr := api.OpenConversationContext(ctx, &goslack.OpenConversationParameters{
					Users: []string{userID},
				})
				if openErr == nil {
					channelID = ch.ID
					channelName = "@" + userName
					sender.Resolver.RegisterChannel(ch.ID, channelName)
					err = nil
				} else {
					return sendResponse{Error: fmt.Sprintf("open DM with %s: %v", userName, openErr)}
				}
			}
		}
		if err != nil {
			var ambErr *slacklistener.AmbiguousChannelError
			if errors.As(err, &ambErr) {
				return sendResponse{Error: formatAmbiguousChannels(ctx, ambErr, sender)}
			}
			return sendResponse{Error: fmt.Sprintf("resolve channel: %v", err)}
		}
	}

	// For bot sends to DMs/group DMs, the cached channel ID is from the user
	// token and isn't accessible to the bot. Open the bot's own conversation.
	if !req.AsUser && strings.HasPrefix(channelName, "@") {
		var userIDs []string

		if strings.HasPrefix(channelName, "@mpdm-") {
			// Group DM: look up members via user token, then open with bot token.
			members, _, err := sender.UserAPI.GetUsersInConversationContext(ctx, &goslack.GetUsersInConversationParameters{
				ChannelID: channelID,
			})
			if err != nil {
				return sendResponse{Error: fmt.Sprintf("get members of %s: %v", channelName, err)}
			}
			userIDs = members
		} else {
			// 1:1 DM: find the target user ID.
			userID, _, userErr := sender.Resolver.FindUserID(channelName)
			if userErr != nil {
				return sendResponse{Error: fmt.Sprintf("resolve user %s: %v", channelName, userErr)}
			}
			userIDs = []string{userID}
		}

		ch, _, _, openErr := sender.BotAPI.OpenConversationContext(ctx, &goslack.OpenConversationParameters{
			Users: userIDs,
		})
		if openErr != nil {
			return sendResponse{Error: fmt.Sprintf("open bot conversation with %s: %v", channelName, openErr)}
		}
		channelID = ch.ID
		senderName = "sent by pigeon"
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
			return sendResponse{Error: fmt.Sprintf(
				"send to %s failed: %v (bot is not a member of this channel — ask the user to invite the bot to %s, or ask the user for permission to re-run with --as-user which sends as the logged-in user instead)",
				channelName, err, channelName)}
		}
		return sendResponse{Error: fmt.Sprintf("send to %s failed: %v", channelName, err)}
	}

	// Store locally.
	msgTS := slacklistener.ParseTimestamp(ts)
	if req.Thread != "" {
		if err := store.WriteThreadMessage("slack", sender.Workspace, channelName, req.Thread, senderName, req.Message, msgTS, true); err != nil {
			slog.ErrorContext(ctx, "failed to store sent thread message", "error", err)
		}
	} else {
		if err := store.WriteMessage("slack", sender.Workspace, channelName, senderName, req.Message, msgTS); err != nil {
			slog.ErrorContext(ctx, "failed to store sent message", "error", err)
		}
	}

	return sendResponse{OK: true, Timestamp: msgTS.Format(time.RFC3339)}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// formatAmbiguousChannels builds a disambiguation message for Slack channels,
// enriched with activity info from the Slack API (last message, open status).
func formatAmbiguousChannels(ctx context.Context, err *slacklistener.AmbiguousChannelError, sender *SlackSender) string {
	var b strings.Builder
	fmt.Fprintf(&b, "multiple channels match %q:\n", err.Query)

	for _, m := range err.Matches {
		fmt.Fprintf(&b, "  %s  %s", m.ID, m.Name)

		ch, apiErr := sender.UserAPI.GetConversationInfoContext(ctx, &goslack.GetConversationInfoInput{
			ChannelID: m.ID,
		})
		if apiErr == nil {
			if ch.Latest != nil && ch.Latest.Timestamp != "" {
				lastMsg := slacklistener.ParseTimestamp(ch.Latest.Timestamp)
				fmt.Fprintf(&b, "  (last msg: %s", lastMsg.Format("2006-01-02"))
				if ch.Latest.Text != "" {
					preview := ch.Latest.Text
					if len(preview) > 50 {
						preview = preview[:50] + "..."
					}
					fmt.Fprintf(&b, ", %q", preview)
				}
				b.WriteString(")")
			} else {
				b.WriteString("  (no messages)")
			}
		}
		b.WriteString("\n")
	}
	b.WriteString("use a channel ID (e.g. D1234567890) to disambiguate")
	return b.String()
}

// formatAmbiguousContacts builds a disambiguation message enriched with
// conversation activity (last message date, total messages) from the file store.
func formatAmbiguousContacts(err *walistener.AmbiguousContactError, account string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "multiple contacts match %q:\n", err.Query)

	for _, m := range err.Matches {
		convDir := m.Phone // conversation directories are "+phone"
		lastDate, msgCount := convActivity("whatsapp", account, convDir)

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
func convActivity(platform, account, conversation string) (lastDate string, totalLines int) {
	dir := filepath.Join(store.DataDir(), platform, account, conversation)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", 0
	}

	var dates []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".txt") {
			dates = append(dates, strings.TrimSuffix(e.Name(), ".txt"))
		}
	}
	if len(dates) == 0 {
		return "", 0
	}
	sort.Strings(dates)

	// Count lines across all files.
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".txt") {
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
