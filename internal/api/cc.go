package api

import (
	"context"
	"encoding/json"
	"log/slog"

	goslack "github.com/slack-go/slack"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/outbox"
)

// isOwnerTarget returns true if the send request targets the owner's own
// DM — e.g., the bot sending a message to the person who installed pigeon.
func (s *Server) isOwnerTarget(req SendRequest) bool {
	if req.Platform != "slack" || req.Slack == nil || req.Slack.UserID == "" {
		return false
	}
	acct := account.New(req.Platform, req.Account)
	s.mu.RLock()
	sender, ok := s.slack[acct.NameSlug()]
	s.mu.RUnlock()
	if !ok {
		return false
	}
	return req.Slack.UserID == sender.UserID
}

// CCNotifier returns an outbox.SubmitFunc that posts a C&C review message
// to the owner's DM in Slack when a new item arrives.
func (s *Server) CCNotifier() outbox.SubmitFunc {
	return func(item *outbox.Item) {
		go s.postCCMessage(item)
	}
}

func (s *Server) postCCMessage(item *outbox.Item) {
	var req SendRequest
	if err := json.Unmarshal(item.Payload, &req); err != nil {
		slog.Error("cc: cannot parse outbox payload", "id", item.ID, "error", err)
		return
	}

	if req.Platform != "slack" {
		slog.Debug("cc: skipping non-Slack outbox item", "platform", req.Platform, "id", item.ID)
		return
	}

	acct := account.New(req.Platform, req.Account)
	s.mu.RLock()
	sender, ok := s.slack[acct.NameSlug()]
	s.mu.RUnlock()
	if !ok {
		slog.Error("cc: no sender for account", "account", acct)
		return
	}

	// Open DM with the owner (the person who installed the user token).
	ctx := context.Background()
	dm, _, _, err := sender.BotAPI.OpenConversationContext(ctx, &goslack.OpenConversationParameters{
		Users: []string{sender.UserID},
	})
	if err != nil {
		slog.Error("cc: failed to open owner DM", "error", err, "account", acct)
		return
	}

	target := req.Target()
	_, _, err = sender.BotAPI.PostMessageContext(ctx, dm.ID,
		goslack.MsgOptionText("Pending review: "+req.Message, false),
		goslack.MsgOptionBlocks(
			goslack.NewSectionBlock(
				goslack.NewTextBlockObject("mrkdwn", req.Message, false, false),
				nil, nil,
			),
			goslack.NewContextBlock("",
				goslack.NewTextBlockObject("mrkdwn", "to "+target, false, false),
			),
			&goslack.ActionBlock{
				Type:    "actions",
				BlockID: "outbox_actions",
				Elements: &goslack.BlockElements{
					ElementSet: []goslack.BlockElement{
						&goslack.ButtonBlockElement{
							Type:     "button",
							ActionID: "outbox_approve",
							Value:    item.ID,
							Text:     goslack.NewTextBlockObject("plain_text", "Approve", false, false),
							Style:    goslack.StylePrimary,
						},
						&goslack.ButtonBlockElement{
							Type:     "button",
							ActionID: "outbox_feedback",
							Value:    item.ID,
							Text:     goslack.NewTextBlockObject("plain_text", "Feedback", false, false),
						},
					},
				},
			},
		),
	)
	if err != nil {
		slog.Error("cc: failed to post review message", "error", err, "outbox_id", item.ID, "account", acct)
	}
}
