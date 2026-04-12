package slack

import (
	"strings"

	goslack "github.com/slack-go/slack"
)

// extractText returns the message text, falling back to blocks and attachments
// when the top-level Text field is empty. Many bot messages carry their content
// exclusively in Block Kit blocks or legacy attachments.
func extractText(text string, blocks goslack.Blocks, attachments []goslack.Attachment) string {
	if text != "" {
		return text
	}
	if t := blocksText(blocks); t != "" {
		return t
	}
	return attachmentsText(attachments)
}

// blocksText walks a Blocks structure and concatenates all human-readable text.
func blocksText(blocks goslack.Blocks) string {
	var b strings.Builder
	for _, block := range blocks.BlockSet {
		switch bl := block.(type) {
		case *goslack.RichTextBlock:
			richTextBlockText(&b, bl)
		case *goslack.SectionBlock:
			sectionBlockText(&b, bl)
		case *goslack.HeaderBlock:
			if bl.Text != nil && bl.Text.Text != "" {
				appendLine(&b, bl.Text.Text)
			}
		case *goslack.ContextBlock:
			contextBlockText(&b, bl)
		}
	}
	return strings.TrimSpace(b.String())
}

func richTextBlockText(b *strings.Builder, block *goslack.RichTextBlock) {
	for _, elem := range block.Elements {
		switch el := elem.(type) {
		case *goslack.RichTextSection:
			richTextSectionText(b, el)
		case *goslack.RichTextList:
			for _, li := range el.Elements {
				if sec, ok := li.(*goslack.RichTextSection); ok {
					appendText(b, "• ")
					richTextSectionText(b, sec)
				}
			}
		case *goslack.RichTextQuote:
			appendText(b, "> ")
			richTextSectionText(b, (*goslack.RichTextSection)(el))
		case *goslack.RichTextPreformatted:
			appendText(b, "```\n")
			richTextSectionText(b, &el.RichTextSection)
			appendText(b, "```\n")
		}
	}
}

func richTextSectionText(b *strings.Builder, sec *goslack.RichTextSection) {
	for _, elem := range sec.Elements {
		switch el := elem.(type) {
		case *goslack.RichTextSectionTextElement:
			appendText(b, el.Text)
		case *goslack.RichTextSectionLinkElement:
			if el.Text != "" {
				appendText(b, el.Text)
			} else {
				appendText(b, el.URL)
			}
		case *goslack.RichTextSectionUserElement:
			appendText(b, "<@"+el.UserID+">")
		case *goslack.RichTextSectionChannelElement:
			appendText(b, "<#"+el.ChannelID+">")
		case *goslack.RichTextSectionEmojiElement:
			appendText(b, ":"+el.Name+":")
		case *goslack.RichTextSectionBroadcastElement:
			appendText(b, "@"+el.Range)
		}
	}
	if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n") {
		b.WriteByte('\n')
	}
}

func sectionBlockText(b *strings.Builder, block *goslack.SectionBlock) {
	if block.Text != nil && block.Text.Text != "" {
		appendLine(b, block.Text.Text)
	}
	for _, f := range block.Fields {
		if f != nil && f.Text != "" {
			appendLine(b, f.Text)
		}
	}
}

func contextBlockText(b *strings.Builder, block *goslack.ContextBlock) {
	for _, elem := range block.ContextElements.Elements {
		if te, ok := elem.(*goslack.TextBlockObject); ok && te.Text != "" {
			appendLine(b, te.Text)
		}
	}
}

// attachmentsText extracts text from legacy attachments.
func attachmentsText(attachments []goslack.Attachment) string {
	var b strings.Builder
	for _, a := range attachments {
		if a.Text != "" {
			appendLine(&b, a.Text)
		} else if a.Fallback != "" {
			appendLine(&b, a.Fallback)
		}
		if a.Pretext != "" {
			appendLine(&b, a.Pretext)
		}
	}
	return strings.TrimSpace(b.String())
}

func appendText(b *strings.Builder, s string) {
	b.WriteString(s)
}

func appendLine(b *strings.Builder, s string) {
	b.WriteString(s)
	if !strings.HasSuffix(s, "\n") {
		b.WriteByte('\n')
	}
}
