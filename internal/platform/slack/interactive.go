package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	goslack "github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"

	"github.com/anish749/pigeon/internal/outbox/ccview"
)

type feedbackMeta struct {
	OutboxID  string `json:"id"`
	ChannelID string `json:"ch"`
	MessageTS string `json:"ts"`
	Message   string `json:"msg,omitempty"`
	Target    string `json:"tgt,omitempty"`
}

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

	view := ccview.FromBlocks(cb.Message.Blocks.BlockSet)

	item := l.obHandler.Get(outboxID)
	if item == nil {
		l.client.Ack(*evt.Request)
		l.updateCCStatus(ctx, channelID, msgTS, "⚠️ Item no longer in outbox", view)
		return
	}

	switch action.ActionID {
	case "outbox_approve":
		l.client.Ack(*evt.Request)
		if err := l.obHandler.Approve(ctx, item); err != nil {
			l.updateCCStatus(ctx, channelID, msgTS, fmt.Sprintf("✗ Send failed: %s", err), view)
		} else {
			l.updateCCStatus(ctx, channelID, msgTS, "✓ Approved and sent", view)
		}

	case "outbox_dismiss":
		l.client.Ack(*evt.Request)
		l.obHandler.Dismiss(item)
		l.updateCCStatus(ctx, channelID, msgTS, "✗ Dismissed", view)

	case "outbox_sendmode":
		l.client.Ack(*evt.Request)
		nextVia := item.CycleVia()
		if err := l.obHandler.SetVia(item, string(nextVia)); err != nil {
			slog.ErrorContext(ctx, "slack: failed to update send mode",
				"error", err, "outbox_id", outboxID, "account", l.acct)
			return
		}
		l.refreshCCMessage(ctx, channelID, msgTS, ccview.FromItem(item, view.Message, view.Target))

	case "outbox_feedback":
		l.client.Ack(*evt.Request)
		modal, err := feedbackModal(outboxID, channelID, msgTS, view.Message, view.Target)
		if err != nil {
			slog.ErrorContext(ctx, "slack: failed to build feedback modal",
				"error", err, "outbox_id", outboxID, "account", l.acct)
			return
		}
		_, err = l.botAPI.OpenViewContext(ctx, cb.TriggerID, modal)
		if err != nil {
			slog.ErrorContext(ctx, "slack: failed to open feedback modal",
				"error", err, "outbox_id", outboxID, "account", l.acct)
		}

	default:
		slog.InfoContext(ctx, "slack: unhandled action_id",
			"action_id", action.ActionID, "account", l.acct)
		l.client.Ack(*evt.Request)
	}
}

func (l *Listener) handleViewSubmission(ctx context.Context, cb goslack.InteractionCallback, evt *socketmode.Event) {
	if cb.View.CallbackID != "outbox_feedback_modal" {
		slog.ErrorContext(ctx, "slack: unhandled view submission",
			"callback_id", cb.View.CallbackID, "account", l.acct)
		l.client.Ack(*evt.Request)
		return
	}

	var meta feedbackMeta
	if err := json.Unmarshal([]byte(cb.View.PrivateMetadata), &meta); err != nil {
		slog.ErrorContext(ctx, "slack: failed to parse feedback modal metadata",
			"error", err, "account", l.acct)
		l.client.Ack(*evt.Request, goslack.NewErrorsViewSubmissionResponse(
			map[string]string{"feedback_note": "Something went wrong — please try again"},
		))
		return
	}
	l.client.Ack(*evt.Request)

	noteBlock, ok := cb.View.State.Values["feedback_note"]
	if !ok {
		return
	}
	note := noteBlock["note"].Value

	view := ccview.View{Message: meta.Message, Target: meta.Target}

	item := l.obHandler.Get(meta.OutboxID)
	if item == nil {
		slog.WarnContext(ctx, "slack: feedback submitted for missing outbox item",
			"outbox_id", meta.OutboxID, "account", l.acct)
		l.updateCCStatus(ctx, meta.ChannelID, meta.MessageTS, "⚠️ Item no longer in outbox", view)
		return
	}

	if err := l.obHandler.Feedback(item, note); err != nil {
		slog.ErrorContext(ctx, "slack: feedback delivery failed",
			"outbox_id", meta.OutboxID, "error", err, "account", l.acct)
		l.updateCCStatus(ctx, meta.ChannelID, meta.MessageTS, "⚠️ Feedback could not be delivered — session not connected", view)
		return
	}

	l.updateCCStatus(ctx, meta.ChannelID, meta.MessageTS, "✓ Feedback sent", view)
	slog.InfoContext(ctx, "slack: feedback delivered via C&C",
		"outbox_id", meta.OutboxID, "account", l.acct)
}

// updateCCStatus replaces the message with text + status line, no buttons.
func (l *Listener) updateCCStatus(ctx context.Context, channelID, ts, status string, view ccview.View) {
	blocks := view.StatusBlocks(status)
	_, _, _, err := l.botAPI.UpdateMessageContext(ctx, channelID, ts,
		goslack.MsgOptionText(status, false),
		goslack.MsgOptionBlocks(blocks...),
	)
	if err != nil {
		slog.ErrorContext(ctx, "slack: failed to update C&C message",
			"error", err, "channel", channelID, "ts", ts, "account", l.acct)
	}
}

// refreshCCMessage replaces the message with updated buttons (e.g. after send mode toggle).
func (l *Listener) refreshCCMessage(ctx context.Context, channelID, ts string, view ccview.View) {
	_, _, _, err := l.botAPI.UpdateMessageContext(ctx, channelID, ts,
		goslack.MsgOptionText(view.FallbackText(), false),
		goslack.MsgOptionBlocks(view.Blocks()...),
	)
	if err != nil {
		slog.ErrorContext(ctx, "slack: failed to refresh C&C message",
			"error", err, "channel", channelID, "ts", ts, "account", l.acct)
	}
}

// feedbackModal builds the modal shown when the user clicks "Feedback".
func feedbackModal(outboxID, channelID, messageTS, message, target string) (goslack.ModalViewRequest, error) {
	meta, err := json.Marshal(feedbackMeta{
		OutboxID:  outboxID,
		ChannelID: channelID,
		MessageTS: messageTS,
		Message:   message,
		Target:    target,
	})
	if err != nil {
		return goslack.ModalViewRequest{}, fmt.Errorf("marshal feedback metadata: %w", err)
	}
	return goslack.ModalViewRequest{
		Type:            "modal",
		CallbackID:      "outbox_feedback_modal",
		PrivateMetadata: string(meta),
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
	}, nil
}
