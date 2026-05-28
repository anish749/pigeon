// Package ccview renders the Slack command-and-control review message
// for pending outbox items. Both the API layer (initial post) and the
// Slack interactive handler (refresh/update) use this package so the
// block layout is defined in one place.
package ccview

import (
	goslack "github.com/slack-go/slack"

	"github.com/anish749/pigeon/internal/outbox"
	"github.com/anish749/pigeon/internal/store/modelv1"
)

// View holds the data needed to render a C&C review message.
type View struct {
	Message   string
	Target    string
	ItemID    string
	SessionID string
	Via       modelv1.Via
}

// FromItem builds a View from an outbox item and the resolved display strings.
func FromItem(item *outbox.Item, message, target string) View {
	return View{
		Message:   message,
		Target:    target,
		ItemID:    item.ID,
		SessionID: item.SessionID,
		Via:       item.Via(),
	}
}

// FromBlocks extracts a View from existing Slack blocks (e.g. from
// cb.Message.Blocks.BlockSet in a block_actions callback). The ItemID,
// SessionID, and Via fields are not populated — call WithItem to fill them.
func FromBlocks(blocks []goslack.Block) View {
	var v View
	for _, b := range blocks {
		switch blk := b.(type) {
		case *goslack.SectionBlock:
			if blk.Text != nil {
				v.Message = blk.Text.Text
			}
		case *goslack.ContextBlock:
			if len(blk.ContextElements.Elements) > 0 {
				if txt, ok := blk.ContextElements.Elements[0].(*goslack.TextBlockObject); ok {
					v.Target = txt.Text
				}
			}
		}
	}
	return v
}

// Blocks returns the full message layout with action buttons.
func (v View) Blocks() []goslack.Block {
	blocks := v.textBlocks()
	blocks = append(blocks, &goslack.ActionBlock{
		Type:    "actions",
		BlockID: "outbox_actions",
		Elements: &goslack.BlockElements{
			ElementSet: v.buttons(),
		},
	})
	return blocks
}

// StatusBlocks returns the message text with a status line and no buttons.
func (v View) StatusBlocks(status string) []goslack.Block {
	blocks := v.textBlocks()
	blocks = append(blocks, goslack.NewContextBlock("",
		goslack.NewTextBlockObject("mrkdwn", status, false, false),
	))
	return blocks
}

// FallbackText returns the plain-text summary used for notifications.
func (v View) FallbackText() string {
	return "Pending review: " + v.Message
}

func (v View) textBlocks() []goslack.Block {
	var blocks []goslack.Block
	if v.Message != "" {
		blocks = append(blocks, goslack.NewSectionBlock(
			goslack.NewTextBlockObject("mrkdwn", v.Message, false, false),
			nil, nil,
		))
	}
	if v.Target != "" {
		blocks = append(blocks, goslack.NewContextBlock("",
			goslack.NewTextBlockObject("mrkdwn", v.Target, false, false),
		))
	}
	return blocks
}

func (v View) buttons() []goslack.BlockElement {
	buttons := []goslack.BlockElement{
		&goslack.ButtonBlockElement{
			Type:     "button",
			ActionID: "outbox_approve",
			Value:    v.ItemID,
			Text:     goslack.NewTextBlockObject("plain_text", "Approve", false, false),
			Style:    goslack.StylePrimary,
		},
		&goslack.ButtonBlockElement{
			Type:     "button",
			ActionID: "outbox_dismiss",
			Value:    v.ItemID,
			Text:     goslack.NewTextBlockObject("plain_text", "Dismiss", false, false),
			Style:    goslack.StyleDanger,
		},
		&goslack.ButtonBlockElement{
			Type:     "button",
			ActionID: "outbox_sendmode",
			Value:    v.ItemID,
			Text:     goslack.NewTextBlockObject("plain_text", SendModeLabel(v.Via), false, false),
		},
	}
	if v.SessionID != "" {
		buttons = append(buttons, &goslack.ButtonBlockElement{
			Type:     "button",
			ActionID: "outbox_feedback",
			Value:    v.ItemID,
			Text:     goslack.NewTextBlockObject("plain_text", "Feedback", false, false),
		})
	}
	return buttons
}

// SendModeLabel returns the display label for the current send mode.
func SendModeLabel(via modelv1.Via) string {
	if via == modelv1.ViaPigeonAsUser {
		return "Send as: user"
	}
	return "Send as: bot"
}
