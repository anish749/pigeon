package slack

import (
	"testing"

	goslack "github.com/slack-go/slack"
)

func TestExtractText_PlainText(t *testing.T) {
	got := extractText("hello world", goslack.Blocks{}, nil)
	if got != "hello world" {
		t.Errorf("got %q, want %q", got, "hello world")
	}
}

func TestExtractText_EmptyEverything(t *testing.T) {
	got := extractText("", goslack.Blocks{}, nil)
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestExtractText_RichTextBlock(t *testing.T) {
	blocks := goslack.Blocks{
		BlockSet: []goslack.Block{
			goslack.NewRichTextBlock("b1",
				goslack.NewRichTextSection(
					goslack.NewRichTextSectionTextElement("alert fired", nil),
				),
			),
		},
	}
	got := extractText("", blocks, nil)
	if got != "alert fired" {
		t.Errorf("got %q, want %q", got, "alert fired")
	}
}

func TestExtractText_SectionBlock(t *testing.T) {
	blocks := goslack.Blocks{
		BlockSet: []goslack.Block{
			&goslack.SectionBlock{
				Type: goslack.MBTSection,
				Text: &goslack.TextBlockObject{Text: "deployment complete"},
			},
		},
	}
	got := extractText("", blocks, nil)
	if got != "deployment complete" {
		t.Errorf("got %q, want %q", got, "deployment complete")
	}
}

func TestExtractText_SectionFields(t *testing.T) {
	blocks := goslack.Blocks{
		BlockSet: []goslack.Block{
			&goslack.SectionBlock{
				Type: goslack.MBTSection,
				Fields: []*goslack.TextBlockObject{
					{Text: "Status: OK"},
					{Text: "Region: us-east-1"},
				},
			},
		},
	}
	got := extractText("", blocks, nil)
	want := "Status: OK\nRegion: us-east-1"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestExtractText_HeaderBlock(t *testing.T) {
	blocks := goslack.Blocks{
		BlockSet: []goslack.Block{
			&goslack.HeaderBlock{
				Type: goslack.MBTHeader,
				Text: &goslack.TextBlockObject{Text: "Incident Report"},
			},
		},
	}
	got := extractText("", blocks, nil)
	if got != "Incident Report" {
		t.Errorf("got %q, want %q", got, "Incident Report")
	}
}

func TestExtractText_Attachments(t *testing.T) {
	attachments := []goslack.Attachment{
		{Text: "build passed"},
	}
	got := extractText("", goslack.Blocks{}, attachments)
	if got != "build passed" {
		t.Errorf("got %q, want %q", got, "build passed")
	}
}

func TestExtractText_AttachmentFallback(t *testing.T) {
	attachments := []goslack.Attachment{
		{Fallback: "CI notification"},
	}
	got := extractText("", goslack.Blocks{}, attachments)
	if got != "CI notification" {
		t.Errorf("got %q, want %q", got, "CI notification")
	}
}

func TestExtractText_BlocksBeforeAttachments(t *testing.T) {
	blocks := goslack.Blocks{
		BlockSet: []goslack.Block{
			goslack.NewRichTextBlock("b1",
				goslack.NewRichTextSection(
					goslack.NewRichTextSectionTextElement("from blocks", nil),
				),
			),
		},
	}
	attachments := []goslack.Attachment{
		{Text: "from attachments"},
	}
	got := extractText("", blocks, attachments)
	if got != "from blocks" {
		t.Errorf("got %q, want %q — blocks should take priority", got, "from blocks")
	}
}

func TestExtractText_PlainTextBeforeBlocks(t *testing.T) {
	blocks := goslack.Blocks{
		BlockSet: []goslack.Block{
			goslack.NewRichTextBlock("b1",
				goslack.NewRichTextSection(
					goslack.NewRichTextSectionTextElement("from blocks", nil),
				),
			),
		},
	}
	got := extractText("plain text", blocks, nil)
	if got != "plain text" {
		t.Errorf("got %q, want %q — plain text should take priority", got, "plain text")
	}
}

func TestExtractText_RichTextWithLinks(t *testing.T) {
	blocks := goslack.Blocks{
		BlockSet: []goslack.Block{
			goslack.NewRichTextBlock("b1",
				goslack.NewRichTextSection(
					goslack.NewRichTextSectionTextElement("see ", nil),
					goslack.NewRichTextSectionLinkElement("https://example.com", "example", nil),
				),
			),
		},
	}
	got := extractText("", blocks, nil)
	if got != "see example" {
		t.Errorf("got %q, want %q", got, "see example")
	}
}

func TestExtractText_RichTextWithUserMention(t *testing.T) {
	blocks := goslack.Blocks{
		BlockSet: []goslack.Block{
			goslack.NewRichTextBlock("b1",
				goslack.NewRichTextSection(
					goslack.NewRichTextSectionTextElement("cc ", nil),
					goslack.NewRichTextSectionUserElement("U123", nil),
				),
			),
		},
	}
	got := extractText("", blocks, nil)
	if got != "cc <@U123>" {
		t.Errorf("got %q, want %q", got, "cc <@U123>")
	}
}

func TestExtractText_MultipleBlocks(t *testing.T) {
	blocks := goslack.Blocks{
		BlockSet: []goslack.Block{
			&goslack.HeaderBlock{
				Type: goslack.MBTHeader,
				Text: &goslack.TextBlockObject{Text: "Alert"},
			},
			&goslack.SectionBlock{
				Type: goslack.MBTSection,
				Text: &goslack.TextBlockObject{Text: "CPU > 90%"},
			},
		},
	}
	got := extractText("", blocks, nil)
	want := "Alert\nCPU > 90%"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
