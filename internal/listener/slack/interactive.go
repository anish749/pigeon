package slack

import (
	"context"
	"fmt"
	"log/slog"

	goslack "github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"
)

// handleInteractive processes block_actions and view_submission events sent
// over Socket Mode when the owner interacts with outbox review messages.
func (l *Listener) handleInteractive(ctx context.Context, evt *socketmode.Event) {
	callback, ok := evt.Data.(goslack.InteractionCallback)
	if !ok {
		slog.WarnContext(ctx, "slack: interactive event had unexpected payload", "account", l.acct)
		l.client.Ack(*evt.Request)
		return
	}

	switch callback.Type {
	case goslack.InteractionTypeBlockActions:
		l.handleBlockAction(ctx, callback, evt)
	case goslack.InteractionTypeViewSubmission:
		l.handleViewSubmission(ctx, callback, evt)
	default:
		slog.InfoContext(ctx, "slack: unhandled interactive type",
			"type", callback.Type, "account", l.acct)
		l.client.Ack(*evt.Request)
	}
}

func (l *Listener) handleBlockAction(ctx context.Context, cb goslack.InteractionCallback, evt *socketmode.Event) {
	if len(cb.ActionCallback.BlockActions) == 0 {
		l.client.Ack(*evt.Request)
		return
	}
	action := cb.ActionCallback.BlockActions[0]
	outboxID := action.Value
	msgTS := cb.Message.Timestamp
	channelID := cb.Channel.ID

	item := l.obHandler.Get(outboxID)
	if item == nil {
		l.client.Ack(*evt.Request)
		l.updateCCMessage(ctx, channelID, msgTS, "⚠️ Item no longer in outbox")
		return
	}

	switch action.ActionID {
	case "outbox_approve":
		l.client.Ack(*evt.Request)
		ok, errMsg := l.obHandler.Approve(ctx, item)
		if ok {
			l.updateCCMessage(ctx, channelID, msgTS, "✓ Approved and sent")
		} else {
			l.updateCCMessage(ctx, channelID, msgTS, fmt.Sprintf("✗ Send failed: %s", errMsg))
		}

	case "outbox_feedback":
		modal := feedbackModal(outboxID)
		_, err := goslack.New(l.botToken).OpenViewContext(ctx, cb.TriggerID, modal)
		if err != nil {
			slog.ErrorContext(ctx, "slack: failed to open feedback modal",
				"error", err, "outbox_id", outboxID, "account", l.acct)
		}
		l.client.Ack(*evt.Request)

	default:
		slog.InfoContext(ctx, "slack: unhandled action_id",
			"action_id", action.ActionID, "account", l.acct)
		l.client.Ack(*evt.Request)
	}
}

func (l *Listener) handleViewSubmission(ctx context.Context, cb goslack.InteractionCallback, evt *socketmode.Event) {
	l.client.Ack(*evt.Request)

	if cb.View.CallbackID != "outbox_feedback_modal" {
		return
	}

	outboxID := cb.View.PrivateMetadata
	noteBlock, ok := cb.View.State.Values["feedback_note"]
	if !ok {
		return
	}
	note := noteBlock["note"].Value

	item := l.obHandler.Get(outboxID)
	if item == nil {
		slog.WarnContext(ctx, "slack: feedback submitted for missing outbox item",
			"outbox_id", outboxID, "account", l.acct)
		return
	}

	if err := l.obHandler.Feedback(item, note); err != nil {
		slog.ErrorContext(ctx, "slack: feedback delivery failed",
			"outbox_id", outboxID, "error", err, "account", l.acct)
		// Notify the owner that feedback couldn't be delivered.
		botAPI := goslack.New(l.botToken)
		dm, _, _, err := botAPI.OpenConversationContext(ctx, &goslack.OpenConversationParameters{
			Users: []string{cb.User.ID},
		})
		if err == nil {
			botAPI.PostMessageContext(ctx, dm.ID,
				goslack.MsgOptionText(fmt.Sprintf("⚠️ Feedback for `%s` could not be delivered — session not connected.", outboxID), false))
		}
		return
	}

	slog.InfoContext(ctx, "slack: feedback delivered via C&C",
		"outbox_id", outboxID, "account", l.acct)
}

// updateCCMessage replaces the original C&C message with a status line and no buttons.
func (l *Listener) updateCCMessage(ctx context.Context, channelID, ts, status string) {
	botAPI := goslack.New(l.botToken)
	_, _, _, err := botAPI.UpdateMessageContext(ctx, channelID, ts,
		goslack.MsgOptionText(status, false),
		goslack.MsgOptionBlocks(
			goslack.NewSectionBlock(
				goslack.NewTextBlockObject("mrkdwn", status, false, false),
				nil, nil,
			),
		),
	)
	if err != nil {
		slog.ErrorContext(ctx, "slack: failed to update C&C message",
			"error", err, "channel", channelID, "ts", ts, "account", l.acct)
	}
}

// feedbackModal builds the modal shown when the user clicks "Feedback".
func feedbackModal(outboxID string) goslack.ModalViewRequest {
	return goslack.ModalViewRequest{
		Type:            "modal",
		CallbackID:      "outbox_feedback_modal",
		PrivateMetadata: outboxID,
		Title:           goslack.NewTextBlockObject("plain_text", "Feedback", false, false),
		Submit:          goslack.NewTextBlockObject("plain_text", "Send feedback", false, false),
		Close:           goslack.NewTextBlockObject("plain_text", "Cancel", false, false),
		Blocks: goslack.Blocks{
			BlockSet: []goslack.Block{
				goslack.NewInputBlock(
					"feedback_note",
					goslack.NewTextBlockObject("plain_text", "What should Claude do differently?", false, false),
					nil,
					&goslack.PlainTextInputBlockElement{
						Type:      "plain_text_input",
						ActionID:  "note",
						Multiline: true,
						Placeholder: goslack.NewTextBlockObject("plain_text",
							"e.g., 'too formal, try again' or 'ask about 2pm instead'", false, false),
					},
				),
			},
		},
	}
}
