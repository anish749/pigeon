package mrkdwn

import "testing"

func TestToSlackMarkdown(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		// === Bold ===
		// Markdown uses **, Slack uses single *
		{
			name: "bold double asterisks to single",
			in:   "this is **bold** text",
			want: "this is *bold* text",
		},
		{
			name: "multiple bold spans",
			in:   "**first** and **second**",
			want: "*first* and *second*",
		},
		{
			name: "bold at start of line",
			in:   "**bold start** of line",
			want: "*bold start* of line",
		},

		// === Italic ===
		{
			name: "asterisk italic to underscore",
			in:   "this is *italic* text",
			want: "this is _italic_ text",
		},
		{
			name: "underscore italic stays as underscore",
			in:   "this is _italic_ text",
			want: "this is _italic_ text",
		},

		// === Strikethrough ===
		// Markdown uses ~~, Slack uses single ~
		{
			name: "strikethrough double tilde to single",
			in:   "this is ~~removed~~ text",
			want: "this is ~removed~ text",
		},

		// === Links ===
		// Markdown [text](url) → Slack <url|text>
		{
			name: "markdown link to slack link",
			in:   "see [the docs](https://example.com) for details",
			want: "see <https://example.com|the docs> for details",
		},

		// === Headings ===
		// Slack has no headings — render as bold
		{
			name: "h1 to bold",
			in:   "# Heading",
			want: "*Heading*",
		},
		{
			name: "h2 to bold",
			in:   "## Subheading",
			want: "*Subheading*",
		},

		// === Lists ===
		{
			name: "unordered list with dashes",
			in:   "- one\n- two\n- three",
			want: "• one\n• two\n• three",
		},
		{
			name: "unordered list with asterisks",
			in:   "* one\n* two\n* three",
			want: "• one\n• two\n• three",
		},
		{
			name: "ordered list",
			in:   "1. one\n2. two\n3. three",
			want: "1. one\n2. two\n3. three",
		},

		// === Code ===
		{
			name: "inline code unchanged",
			in:   "run `go test` to verify",
			want: "run `go test` to verify",
		},
		{
			name: "fenced code block",
			in:   "```go\nfmt.Println(\"hi\")\n```",
			want: "```\nfmt.Println(\"hi\")\n```",
		},

		// === Blockquotes ===
		{
			name: "blockquote",
			in:   "> quoted text",
			want: "> quoted text",
		},

		// === Already-valid mrkdwn (no change expected) ===
		{
			name: "single tilde strikethrough unchanged",
			in:   "this is ~struck~ text",
			want: "this is ~struck~ text",
		},
		{
			name: "plain text unchanged",
			in:   "nothing special here",
			want: "nothing special here",
		},
		{
			name: "empty string",
			in:   "",
			want: "",
		},

		// === Combined / realistic messages ===
		// These verify that different formatting types don't interfere with
		// each other — the exact class of bug that regex converters get wrong.
		{
			name: "mixed bold strikethrough code",
			in:   "**Status update:** the ~~old~~ approach is replaced with `newFunc()`",
			want: "*Status update:* the ~old~ approach is replaced with `newFunc()`",
		},
		{
			name: "bold at line start does not become bullet",
			in:   "**first** and **second**\n\n**third** too",
			want: "*first* and *second*\n\n*third* too",
		},

		// === Slack mrkdwn input ===
		// The converter assumes standard Markdown input. When Slack mrkdwn
		// is passed directly, most syntax survives unchanged because the
		// Markdown parser doesn't recognise it. The one known exception is
		// *bold* — Markdown parses single * as italic and converts it to _.
		{
			name: "slack mrkdwn: known bug — *bold* is treated as italic",
			in:   "*bold*",
			want: "_bold_",
		},
		{
			name: "slack mrkdwn: _italic_ passes through",
			in:   "_italic_",
			want: "_italic_",
		},
		{
			name: "slack mrkdwn: ~strike~ passes through",
			in:   "~strike~",
			want: "~strike~",
		},
		{
			name: "slack mrkdwn: inline code passes through",
			in:   "`code`",
			want: "`code`",
		},
		{
			name: "slack mrkdwn: link passes through",
			in:   "<https://example.com|click here>",
			want: "<https://example.com|click here>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ToSlackMarkdown(tt.in)
			if got != tt.want {
				t.Errorf("ToSlackMarkdown(%q)\n  got:  %q\n  want: %q", tt.in, got, tt.want)
			}
		})
	}
}
