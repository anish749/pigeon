package slackraw

import (
	"testing"

	goslack "github.com/slack-go/slack"
)

// richText builds a single-block rich_text Blocks value from one section.
func richText(elements ...goslack.RichTextSectionElement) goslack.Blocks {
	return goslack.Blocks{
		BlockSet: []goslack.Block{
			goslack.NewRichTextBlock("blk", goslack.NewRichTextSection(elements...)),
		},
	}
}

func TestBlocksEquivalentToText(t *testing.T) {
	tests := []struct {
		name    string
		blocks  goslack.Blocks
		rawText string
		want    bool
	}{
		{
			name: "plain text",
			blocks: richText(
				goslack.NewRichTextSectionTextElement("Hello world", nil),
			),
			rawText: "Hello world",
			want:    true,
		},
		{
			name: "text + link + code span",
			blocks: richText(
				goslack.NewRichTextSectionTextElement("see ", nil),
				goslack.NewRichTextSectionLinkElement("https://example.com/docs", "", nil),
				goslack.NewRichTextSectionTextElement(" and run ", nil),
				goslack.NewRichTextSectionTextElement("make build", &goslack.RichTextSectionTextStyle{Code: true}),
			),
			rawText: "see <https://example.com/docs> and run `make build`",
			want:    true,
		},
		{
			name: "user mention",
			blocks: richText(
				goslack.NewRichTextSectionTextElement("hey ", nil),
				goslack.NewRichTextSectionUserElement("U123ABC", nil),
				goslack.NewRichTextSectionTextElement(" ping", nil),
			),
			rawText: "hey <@U123ABC> ping",
			want:    true,
		},
		{
			name: "channel mention",
			blocks: richText(
				goslack.NewRichTextSectionTextElement("see ", nil),
				goslack.NewRichTextSectionChannelElement("C9988", nil),
			),
			rawText: "see <#C9988>",
			want:    true,
		},
		{
			name: "usergroup mention",
			blocks: richText(
				goslack.NewRichTextSectionUserGroupElement("S0001"),
				goslack.NewRichTextSectionTextElement(" heads up", nil),
			),
			rawText: "<!subteam^S0001> heads up",
			want:    true,
		},
		{
			name: "link with explicit anchor text",
			blocks: richText(
				goslack.NewRichTextSectionTextElement("docs: ", nil),
				goslack.NewRichTextSectionLinkElement("https://example.com", "example", nil),
			),
			rawText: "docs: <https://example.com|example>",
			want:    true,
		},
		{
			name: "emoji",
			blocks: richText(
				goslack.NewRichTextSectionTextElement("lgtm ", nil),
				goslack.NewRichTextSectionEmojiElement("thumbsup", 0, nil),
			),
			rawText: "lgtm :thumbsup:",
			want:    true,
		},
		{
			name: "broadcast @channel",
			blocks: richText(
				goslack.NewRichTextSectionBroadcastElement("channel"),
				goslack.NewRichTextSectionTextElement(" deploy done", nil),
			),
			rawText: "<!channel> deploy done",
			want:    true,
		},
		{
			name: "bold span wraps as *x*",
			blocks: richText(
				goslack.NewRichTextSectionTextElement("warning", &goslack.RichTextSectionTextStyle{Bold: true}),
			),
			rawText: "*warning*",
			want:    true,
		},
		{
			name: "italic span wraps as _x_",
			blocks: richText(
				goslack.NewRichTextSectionTextElement("nb", &goslack.RichTextSectionTextStyle{Italic: true}),
			),
			rawText: "_nb_",
			want:    true,
		},
		{
			name: "strike span wraps as ~x~",
			blocks: richText(
				goslack.NewRichTextSectionTextElement("wrong", &goslack.RichTextSectionTextStyle{Strike: true}),
			),
			rawText: "~wrong~",
			want:    true,
		},
		{
			name: "html-escape ampersand in plain text",
			blocks: richText(
				goslack.NewRichTextSectionTextElement("UK & EU", nil),
			),
			rawText: "UK &amp; EU",
			want:    true,
		},
		{
			name: "html-escape angle brackets",
			blocks: richText(
				goslack.NewRichTextSectionTextElement("a<b && c>d", nil),
			),
			rawText: "a&lt;b &amp;&amp; c&gt;d",
			want:    true,
		},
		{
			name: "html-escape inside link anchor",
			blocks: richText(
				goslack.NewRichTextSectionLinkElement("https://e.com/?a=1&b=2", "x&y", nil),
			),
			rawText: "<https://e.com/?a=1&amp;b=2|x&amp;y>",
			want:    true,
		},
		{
			name: "mismatched text returns false",
			blocks: richText(
				goslack.NewRichTextSectionTextElement("hello", nil),
			),
			rawText: "hola",
			want:    false,
		},
		{
			name: "list block is not equivalent",
			blocks: goslack.Blocks{
				BlockSet: []goslack.Block{
					goslack.NewRichTextBlock("blk",
						goslack.NewRichTextList(goslack.RTEListBullet, 0,
							goslack.NewRichTextSection(
								goslack.NewRichTextSectionTextElement("one", nil),
							),
						),
					),
				},
			},
			rawText: "• one",
			want:    false,
		},
		{
			name: "multiple blocks not equivalent",
			blocks: goslack.Blocks{
				BlockSet: []goslack.Block{
					goslack.NewRichTextBlock("a",
						goslack.NewRichTextSection(goslack.NewRichTextSectionTextElement("one", nil)),
					),
					goslack.NewRichTextBlock("b",
						goslack.NewRichTextSection(goslack.NewRichTextSectionTextElement("two", nil)),
					),
				},
			},
			rawText: "one\ntwo",
			want:    false,
		},
		{
			name:    "no blocks",
			blocks:  goslack.Blocks{},
			rawText: "hi",
			want:    false,
		},
		{
			name: "non-rich-text block (section) not equivalent",
			blocks: goslack.Blocks{
				BlockSet: []goslack.Block{
					goslack.NewSectionBlock(
						goslack.NewTextBlockObject(goslack.MarkdownType, "hi", false, false),
						nil, nil,
					),
				},
			},
			rawText: "hi",
			want:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := BlocksEquivalentToText(tc.blocks, tc.rawText)
			if got != tc.want {
				t.Errorf("BlocksEquivalentToText = %v, want %v", got, tc.want)
			}
		})
	}
}
