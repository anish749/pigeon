package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	goslack "github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"
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

	origBlocks := cb.Message.Blocks.BlockSet

	item := l.obHandler.Get(outboxID)
	if item == nil {
		l.client.Ack(*evt.Request)
		l.updateCCMessage(ctx, channelID, msgTS, "⚠️ Item no longer in outbox", origBlocks)
		return
	}

	switch action.ActionID {
	case "outbox_approve":
		l.client.Ack(*evt.Request)
		if err := l.obHandler.Approve(ctx, item); err != nil {
			l.updateCCMessage(ctx, channelID, msgTS, fmt.Sprintf("✗ Send failed: %s", err), origBlocks)
		} else {
			l.updateCCMessage(ctx, channelID, msgTS, "✓ Approved and sent", origBlocks)
		}

	case "outbox_feedback":
		msgText, tgtText := extractCCText(origBlocks)
		modal, err := feedbackModal(outboxID, channelID, msgTS, msgText, tgtText)
		if err != nil {
			slog.ErrorContext(ctx, "slack: failed to build feedback modal",
				"error", err, "outbox_id", outboxID, "account", l.acct)
			l.client.Ack(*evt.Request)
			return
		}
		_, err = l.botAPI.OpenViewContext(ctx, cb.TriggerID, modal)
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

	origBlocks := buildCCBlocks(meta.Message, meta.Target)

	item := l.obHandler.Get(meta.OutboxID)
	if item == nil {
		slog.WarnContext(ctx, "slack: feedback submitted for missing outbox item",
			"outbox_id", meta.OutboxID, "account", l.acct)
		l.updateCCMessage(ctx, meta.ChannelID, meta.MessageTS, "⚠️ Item no longer in outbox", origBlocks)
		return
	}

	if err := l.obHandler.Feedback(item, note); err != nil {
		slog.ErrorContext(ctx, "slack: feedback delivery failed",
			"outbox_id", meta.OutboxID, "error", err, "account", l.acct)
		l.updateCCMessage(ctx, meta.ChannelID, meta.MessageTS, "⚠️ Feedback could not be delivered — session not connected", origBlocks)
		return
	}

	l.updateCCMessage(ctx, meta.ChannelID, meta.MessageTS, "✓ Feedback sent", origBlocks)
	slog.InfoContext(ctx, "slack: feedback delivered via C&C",
		"outbox_id", meta.OutboxID, "account", l.acct)
}

// updateCCMessage strips action buttons from the original message and appends a status line.
func (l *Listener) updateCCMessage(ctx context.Context, channelID, ts, status string, origBlocks []goslack.Block) {
	var kept []goslack.Block
	for _, b := range origBlocks {
		if b.BlockType() == goslack.MBTAction {
			continue
		}
		kept = append(kept, b)
	}
	kept = append(kept, goslack.NewContextBlock("",
		goslack.NewTextBlockObject("mrkdwn", status, false, false),
	))

	_, _, _, err := l.botAPI.UpdateMessageContext(ctx, channelID, ts,
		goslack.MsgOptionText(status, false),
		goslack.MsgOptionBlocks(kept...),
	)
	if err != nil {
		slog.ErrorContext(ctx, "slack: failed to update C&C message",
			"error", err, "channel", channelID, "ts", ts, "account", l.acct)
	}
}

// extractCCText pulls the message and target strings from the C&C message blocks.
func extractCCText(blocks []goslack.Block) (message, target string) {
	for _, b := range blocks {
		switch blk := b.(type) {
		case *goslack.SectionBlock:
			if blk.Text != nil {
				message = blk.Text.Text
			}
		case *goslack.ContextBlock:
			if len(blk.ContextElements.Elements) > 0 {
				if txt, ok := blk.ContextElements.Elements[0].(*goslack.TextBlockObject); ok {
					target = txt.Text
				}
			}
		}
	}
	return message, target
}

// buildCCBlocks reconstructs the non-action C&C blocks from stored text.
func buildCCBlocks(message, target string) []goslack.Block {
	var blocks []goslack.Block
	if message != "" {
		blocks = append(blocks, goslack.NewSectionBlock(
			goslack.NewTextBlockObject("mrkdwn", message, false, false),
			nil, nil,
		))
	}
	if target != "" {
		blocks = append(blocks, goslack.NewContextBlock("",
			goslack.NewTextBlockObject("mrkdwn", target, false, false),
		))
	}
	return blocks
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
