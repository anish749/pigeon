package slackraw

import (
	"fmt"
	"strings"

	goslack "github.com/slack-go/slack"
)

// BlocksEquivalentToText reports whether bs is a single rich_text block whose
// rendering back to Slack mrkdwn equals rawText exactly. When true, storing
// blocks alongside text is redundant — they encode the same content.
//
// rawText must be the pre-resolve message text (user and channel mentions in
// <@Uxx>/<#Cxx> wire form), not the resolved @display-name form, because
// blocks also carry the wire form.
//
// Conservative by design: any unknown element, styled span (bold/italic/
// strike), list, quote, preformatted block, or mismatched string returns
// false. The caller should then keep Blocks in storage.
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
// harness, which needs to show both sides of a mismatch. Production code
// should use BlocksEquivalentToText.
func RenderRichTextForVerify(rt *goslack.RichTextBlock) (string, bool) {
	return renderRichText(rt)
}

// renderRichText converts a rich_text block back to the mrkdwn string Slack
// would have generated as the message's text fallback. Returns (_, false)
// for any construct we don't handle — the caller treats that as non-equivalent.
func renderRichText(rt *goslack.RichTextBlock) (string, bool) {
	var b strings.Builder
	for i, el := range rt.Elements {
		if i > 0 {
			b.WriteByte('\n')
		}
		switch e := el.(type) {
		case *goslack.RichTextSection:
			if !renderSection(&b, e) {
				return "", false
			}
		default:
			// Lists, quotes, preformatted: text fallback format differs from
			// a plain concatenation, so don't claim equivalence.
			return "", false
		}
	}
	return b.String(), true
}

func renderSection(b *strings.Builder, s *goslack.RichTextSection) bool {
	for _, el := range s.Elements {
		switch e := el.(type) {
		case *goslack.RichTextSectionTextElement:
			if !renderText(b, e) {
				return false
			}
		case *goslack.RichTextSectionUserElement:
			fmt.Fprintf(b, "<@%s>", e.UserID)
		case *goslack.RichTextSectionChannelElement:
			fmt.Fprintf(b, "<#%s>", e.ChannelID)
		case *goslack.RichTextSectionUserGroupElement:
			fmt.Fprintf(b, "<!subteam^%s>", e.UsergroupID)
		case *goslack.RichTextSectionLinkElement:
			if e.Text == "" {
				fmt.Fprintf(b, "<%s>", e.URL)
			} else {
				fmt.Fprintf(b, "<%s|%s>", e.URL, e.Text)
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

func renderText(b *strings.Builder, e *goslack.RichTextSectionTextElement) bool {
	if e.Style == nil {
		b.WriteString(e.Text)
		return true
	}
	// Styled spans: only `code` has a stable, round-trippable mrkdwn form
	// (`backticks`). Bold/italic/strike don't always appear in the text
	// fallback, so treat them as non-equivalent.
	if e.Style.Bold || e.Style.Italic || e.Style.Strike {
		return false
	}
	if e.Style.Code {
		b.WriteByte('`')
		b.WriteString(e.Text)
		b.WriteByte('`')
		return true
	}
	b.WriteString(e.Text)
	return true
}
