// Package ccview renders the Slack command-and-control review message
// for pending tool gate items. Both the API layer (initial post) and the
// Slack interactive handler (refresh/update) use this package so the
// block layout is defined in one place.
package ccview

import (
	goslack "github.com/slack-go/slack"

	"github.com/anish749/pigeon/internal/toolgate"
)

// View holds the data needed to render a tool gate C&C review message.
type View struct {
	ToolName string
	Command  string
	CWD      string
	ItemID   string
}

// FromItem builds a View from a tool gate item.
func FromItem(item *toolgate.Item) View {
	return View{
		ToolName: item.ToolName(),
		Command:  item.Command(),
		CWD:      item.Input.CWD,
		ItemID:   item.ID,
	}
}

// FromBlocks extracts a View from existing Slack blocks (e.g. from
// cb.Message.Blocks.BlockSet in a block_actions callback).
func FromBlocks(blocks []goslack.Block) View {
	var v View
	for _, b := range blocks {
		switch blk := b.(type) {
		case *goslack.SectionBlock:
			if blk.Text != nil {
				v.Command = blk.Text.Text
			}
		}
	}
	return v
}

// Blocks returns the full message layout with action buttons.
func (v View) Blocks() []goslack.Block {
	text := "*Tool:* `" + v.ToolName + "`\n```\n" + v.Command + "\n```"
	section := goslack.NewSectionBlock(
		goslack.NewTextBlockObject(goslack.MarkdownType, text, false, false),
		nil, nil,
	)
	ctx := goslack.NewContextBlock("",
		goslack.NewTextBlockObject(goslack.MarkdownType, "cwd: "+v.CWD, false, false),
	)
	actions := goslack.NewActionBlock("",
		v.buttons()...,
	)
	return []goslack.Block{section, ctx, actions}
}

// StatusBlocks returns the message text with a status line and no buttons.
func (v View) StatusBlocks(status string) []goslack.Block {
	text := "*Tool:* `" + v.ToolName + "`\n```\n" + v.Command + "\n```"
	section := goslack.NewSectionBlock(
		goslack.NewTextBlockObject(goslack.MarkdownType, text, false, false),
		nil, nil,
	)
	statusBlock := goslack.NewContextBlock("",
		goslack.NewTextBlockObject(goslack.MarkdownType, status, false, false),
	)
	return []goslack.Block{section, statusBlock}
}

// FallbackText returns the plain-text summary used for notifications.
func (v View) FallbackText() string {
	return "Tool review: " + v.ToolName + " — " + v.Command
}

func (v View) buttons() []goslack.BlockElement {
	allow := goslack.NewButtonBlockElement("toolgate_allow", v.ItemID,
		goslack.NewTextBlockObject(goslack.PlainTextType, "Allow", false, false),
	)
	allow.Style = goslack.StylePrimary
	deny := goslack.NewButtonBlockElement("toolgate_deny", v.ItemID,
		goslack.NewTextBlockObject(goslack.PlainTextType, "Deny", false, false),
	)
	deny.Style = goslack.StyleDanger
	skip := goslack.NewButtonBlockElement("toolgate_ask", v.ItemID,
		goslack.NewTextBlockObject(goslack.PlainTextType, "Skip", false, false),
	)
	return []goslack.BlockElement{allow, deny, skip}
}
