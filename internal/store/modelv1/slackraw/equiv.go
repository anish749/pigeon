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
//   - Bold / Italic / Strike / Code styles wrap as *x* / _x_ / ~x~ / `x`.
//     Consecutive elements sharing a style share a single wrapper: Slack
//     emits `~<@U> text~`, not `~<@U>~~ text~`, when a user mention and a
//     text span both carry strike.
//   - Any section element kind can carry Style (text, user, channel, emoji,
//     link, team). Usergroup and broadcast do not.
//   - `&`, `<`, `>` in text content and link URLs/anchors are HTML-escaped
//     to `&amp;`, `&lt;`, `&gt;`.
//   - user / channel / usergroup / broadcast elements render in wire form.
//   - link elements render as <url> or <url|anchor>.
//   - emoji elements render as :name:.
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

var htmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

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

// renderSection walks a rich_text_section, emitting style wrappers at run
// boundaries. Adjacent elements sharing a style flag share one wrapper.
// Wrappers nest in the order bold → italic → strike → code (outer → inner),
// mirroring how Slack composes its text fallback.
func renderSection(b *strings.Builder, s *goslack.RichTextSection) bool {
	var bold, italic, strike, code bool
	for _, el := range s.Elements {
		st := elementStyle(el)
		// Close styles being turned off, innermost first.
		if code && (st == nil || !st.Code) {
			b.WriteByte('`')
			code = false
		}
		if strike && (st == nil || !st.Strike) {
			b.WriteByte('~')
			strike = false
		}
		if italic && (st == nil || !st.Italic) {
			b.WriteByte('_')
			italic = false
		}
		if bold && (st == nil || !st.Bold) {
			b.WriteByte('*')
			bold = false
		}
		// Open styles being turned on, outermost first.
		if st != nil {
			if st.Bold && !bold {
				b.WriteByte('*')
				bold = true
			}
			if st.Italic && !italic {
				b.WriteByte('_')
				italic = true
			}
			if st.Strike && !strike {
				b.WriteByte('~')
				strike = true
			}
			if st.Code && !code {
				b.WriteByte('`')
				code = true
			}
		}
		if !renderElementContent(b, el) {
			return false
		}
	}
	if code {
		b.WriteByte('`')
	}
	if strike {
		b.WriteByte('~')
	}
	if italic {
		b.WriteByte('_')
	}
	if bold {
		b.WriteByte('*')
	}
	return true
}

func elementStyle(el goslack.RichTextSectionElement) *goslack.RichTextSectionTextStyle {
	switch e := el.(type) {
	case *goslack.RichTextSectionTextElement:
		return e.Style
	case *goslack.RichTextSectionUserElement:
		return e.Style
	case *goslack.RichTextSectionChannelElement:
		return e.Style
	case *goslack.RichTextSectionEmojiElement:
		return e.Style
	case *goslack.RichTextSectionLinkElement:
		return e.Style
	case *goslack.RichTextSectionTeamElement:
		return e.Style
	}
	return nil
}

// renderElementContent writes one element's content with no style wrappers.
// The caller is responsible for emitting wrappers around runs.
func renderElementContent(b *strings.Builder, el goslack.RichTextSectionElement) bool {
	switch e := el.(type) {
	case *goslack.RichTextSectionTextElement:
		b.WriteString(htmlEscaper.Replace(e.Text))
	case *goslack.RichTextSectionUserElement:
		fmt.Fprintf(b, "<@%s>", e.UserID)
	case *goslack.RichTextSectionChannelElement:
		fmt.Fprintf(b, "<#%s>", e.ChannelID)
	case *goslack.RichTextSectionUserGroupElement:
		fmt.Fprintf(b, "<!subteam^%s>", e.UsergroupID)
	case *goslack.RichTextSectionLinkElement:
		if e.Text == "" {
			fmt.Fprintf(b, "<%s>", htmlEscaper.Replace(e.URL))
		} else {
			fmt.Fprintf(b, "<%s|%s>", htmlEscaper.Replace(e.URL), htmlEscaper.Replace(e.Text))
		}
	case *goslack.RichTextSectionEmojiElement:
		fmt.Fprintf(b, ":%s:", e.Name)
	case *goslack.RichTextSectionBroadcastElement:
		fmt.Fprintf(b, "<!%s>", e.Range)
	default:
		return false
	}
	return true
}
