package slackraw

import (
	"fmt"
	"strings"

	goslack "github.com/slack-go/slack"
)

// BlocksEquivalentToText reports whether bs is a single rich_text block whose
// rendering back to Slack's text fallback equals rawText exactly. When true,
// storing blocks alongside text is redundant — they encode the same content.
//
// rawText must be the pre-resolve message text (user and channel mentions in
// <@Uxx>/<#Cxx> wire form), because blocks also carry the wire form. Callers
// that only have post-resolve text should not use this function — the
// comparison will under-report equivalence for messages that contain mentions.
//
// The renderer's rules were derived empirically from Slack's behavior on
// ~16k single-rich_text messages across five workspaces:
//
//   - text spans with Bold/Italic/Strike/Code styles wrap as *x* / _x_ / ~x~ / `x`
//   - & < > in text content are HTML-escaped to &amp; / &lt; / &gt;
//   - user / channel / usergroup / broadcast elements render in wire form
//   - link elements render as <url> or <url|anchor>
//   - emoji elements render as :name:
//
// Elements we don't model (list, quote, preformatted) produce a different
// text fallback; for those we return false and keep the blocks in storage.
func BlocksEquivalentToText(bs goslack.Blocks, rawText string) bool {
	if len(bs.BlockSet) != 1 {
		return false
	}
	rt, ok := bs.BlockSet[0].(*goslack.RichTextBlock)
	if !ok {
		return false
	}
	rendered, ok := renderRichText(rt)
	if !ok {
		return false
	}
	return rendered == rawText
}

// RenderRichTextForVerify is exported only for the cmd/verify-slack-equiv
// harness. Production code should use BlocksEquivalentToText.
func RenderRichTextForVerify(rt *goslack.RichTextBlock) (string, bool) {
	return renderRichText(rt)
}

// renderRichText converts a rich_text block back to Slack's text fallback.
// Returns (_, false) for any construct we don't model — lists, quotes, and
// rich_text_preformatted are the known-unhandled kinds.
func renderRichText(rt *goslack.RichTextBlock) (string, bool) {
	var b strings.Builder
	for i, el := range rt.Elements {
		if i > 0 {
			b.WriteByte('\n')
		}
		sec, ok := el.(*goslack.RichTextSection)
		if !ok {
			return "", false
		}
		if !renderSection(&b, sec) {
			return "", false
		}
	}
	return b.String(), true
}

func renderSection(b *strings.Builder, s *goslack.RichTextSection) bool {
	for _, el := range s.Elements {
		switch e := el.(type) {
		case *goslack.RichTextSectionTextElement:
			b.WriteString(wrapStyledText(e))
		case *goslack.RichTextSectionUserElement:
			fmt.Fprintf(b, "<@%s>", e.UserID)
		case *goslack.RichTextSectionChannelElement:
			fmt.Fprintf(b, "<#%s>", e.ChannelID)
		case *goslack.RichTextSectionUserGroupElement:
			fmt.Fprintf(b, "<!subteam^%s>", e.UsergroupID)
		case *goslack.RichTextSectionLinkElement:
			if e.Text == "" {
				fmt.Fprintf(b, "<%s>", escapeHTML(e.URL))
			} else {
				fmt.Fprintf(b, "<%s|%s>", escapeHTML(e.URL), escapeHTML(e.Text))
			}
		case *goslack.RichTextSectionEmojiElement:
			fmt.Fprintf(b, ":%s:", e.Name)
		case *goslack.RichTextSectionBroadcastElement:
			fmt.Fprintf(b, "<!%s>", e.Range)
		default:
			return false
		}
	}
	return true
}

// wrapStyledText applies Slack's text-fallback style wrappers in the order
// Slack composes them (code innermost, then strike, italic, bold outermost).
func wrapStyledText(e *goslack.RichTextSectionTextElement) string {
	text := escapeHTML(e.Text)
	if e.Style == nil {
		return text
	}
	if e.Style.Code {
		text = "`" + text + "`"
	}
	if e.Style.Strike {
		text = "~" + text + "~"
	}
	if e.Style.Italic {
		text = "_" + text + "_"
	}
	if e.Style.Bold {
		text = "*" + text + "*"
	}
	return text
}

// escapeHTML applies the same &/</> escaping Slack applies to the text field.
var htmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

func escapeHTML(s string) string {
	return htmlEscaper.Replace(s)
}
