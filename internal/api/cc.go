package api

import (
	"context"
	"encoding/json"
	"fmt"

	goslack "github.com/slack-go/slack"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/outbox"
	"github.com/anish749/pigeon/internal/outbox/ccview"
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

// postCCMessage posts a review message to the owner's DM in Slack when a
// new outbox item arrives. Returns nil for non-Slack items (no notification
// needed). Returns an error if the Slack API call fails so the caller can
// surface it to the CLI user.
func (s *Server) postCCMessage(ctx context.Context, item *outbox.Item) error {
	var resolved ResolvedSendRequest
	if err := json.Unmarshal(item.Payload, &resolved); err != nil {
		return fmt.Errorf("cc: parse outbox payload: %w", err)
	}

	if resolved.Platform != "slack" {
		return nil
	}

	acct := account.New(resolved.Platform, resolved.Account)
	s.mu.RLock()
	sender, ok := s.slack[acct.NameSlug()]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("cc: no sender for account %s", acct)
	}

	dm, _, _, err := sender.BotAPI.OpenConversationContext(ctx, &goslack.OpenConversationParameters{
		Users: []string{sender.UserID},
	})
	if err != nil {
		return fmt.Errorf("cc: open owner DM: %w", err)
	}

	view := ccview.FromItem(item, resolved.FinalMessage(), "to "+resolved.ResolvedTarget())

	_, _, err = sender.BotAPI.PostMessageContext(ctx, dm.ID,
		goslack.MsgOptionText(view.FallbackText(), false),
		goslack.MsgOptionBlocks(view.Blocks()...),
	)
	if err != nil {
		return fmt.Errorf("cc: post review message: %w", err)
	}
	return nil
}
