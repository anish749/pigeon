package slackraw

import (
	"encoding/json"
	"testing"

	goslack "github.com/slack-go/slack"
)

func TestNewSlackRawContent_Empty(t *testing.T) {
	rc := NewSlackRawContent(goslack.Msg{})
	raw := rc.AsSerializable()
	if raw != nil {
		t.Errorf("empty msg raw = %v, want nil", raw)
	}
}

func TestNewSlackRawContent_Files(t *testing.T) {
	msg := goslack.Msg{
		Files: []goslack.File{
			{Name: "plan.md", Mimetype: "text/plain", Size: 3636},
		},
	}
	rc := NewSlackRawContent(msg)
	raw := rc.AsSerializable()
	if raw == nil {
		t.Fatal("raw is nil, want non-nil")
	}
	files, ok := raw["files"].([]any)
	if !ok || len(files) != 1 {
		t.Fatalf("raw[files] = %v, want slice of 1", raw["files"])
	}
	file := files[0].(map[string]any)
	if file["name"] != "plan.md" {
		t.Errorf("file name = %v, want plan.md", file["name"])
	}
}

func TestNewSlackRawContent_Blocks(t *testing.T) {
	msg := goslack.Msg{
		Blocks: goslack.Blocks{
			BlockSet: []goslack.Block{
				goslack.NewRichTextBlock("blk1",
					goslack.NewRichTextSection(
						goslack.NewRichTextSectionTextElement("A huddle started", nil),
					),
				),
			},
		},
	}
	rc := NewSlackRawContent(msg)
	raw := rc.AsSerializable()
	if raw == nil {
		t.Fatal("raw is nil, want non-nil")
	}
	if _, ok := raw["blocks"]; !ok {
		t.Error("raw[blocks] missing")
	}
}

// blocksFromText is a helper that builds a single rich_text block whose
// only content is one text element. Used to keep table cases readable.
func blocksFromText(s string) goslack.Blocks {
	return goslack.Blocks{
		BlockSet: []goslack.Block{
			goslack.NewRichTextBlock("blk",
				goslack.NewRichTextSection(
					goslack.NewRichTextSectionTextElement(s, nil),
				),
			),
		},
	}
}

func TestNewSlackRawContent_BlockEquivalence(t *testing.T) {
	tests := []struct {
		name         string
		msg          goslack.Msg
		wantBlocks   bool
		wantAttaches int
		wantFiles    int
	}{
		{
			name: "blocks render to text — dropped",
			msg: goslack.Msg{
				Text:   "Hello world",
				Blocks: blocksFromText("Hello world"),
			},
			wantBlocks: false,
		},
		{
			name: "blocks diverge from text — kept (huddle system message)",
			msg: goslack.Msg{
				Text:   "",
				Blocks: blocksFromText("A huddle started"),
			},
			wantBlocks: true,
		},
		{
			name: "equivalent blocks + attachments — blocks dropped, attachments preserved",
			msg: goslack.Msg{
				Text:        "see the attached",
				Blocks:      blocksFromText("see the attached"),
				Attachments: []goslack.Attachment{{Title: "PR", Fallback: "PR fallback"}},
			},
			wantBlocks:   false,
			wantAttaches: 1,
		},
		{
			name: "equivalent blocks + files — blocks dropped, files preserved",
			msg: goslack.Msg{
				Text:   "image attached",
				Blocks: blocksFromText("image attached"),
				Files:  []goslack.File{{Name: "img.png", Mimetype: "image/png"}},
			},
			wantBlocks: false,
			wantFiles:  1,
		},
		{
			// In production, msg.Text is the pre-resolve wire form, so a
			// user-mention in blocks lines up byte-for-byte with the text.
			name: "wire-form mention in text — equivalent, dropped",
			msg: goslack.Msg{
				Text: "hey <@U123ABC> ping",
				Blocks: goslack.Blocks{
					BlockSet: []goslack.Block{
						goslack.NewRichTextBlock("blk",
							goslack.NewRichTextSection(
								goslack.NewRichTextSectionTextElement("hey ", nil),
								goslack.NewRichTextSectionUserElement("U123ABC", nil),
								goslack.NewRichTextSectionTextElement(" ping", nil),
							),
						),
					},
				},
			},
			wantBlocks: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rc := NewSlackRawContent(tc.msg)
			if (rc.Blocks != nil) != tc.wantBlocks {
				t.Errorf("Blocks present = %v, want %v", rc.Blocks != nil, tc.wantBlocks)
			}
			if len(rc.Attachments) != tc.wantAttaches {
				t.Errorf("Attachments count = %d, want %d", len(rc.Attachments), tc.wantAttaches)
			}
			if len(rc.Files) != tc.wantFiles {
				t.Errorf("Files count = %d, want %d", len(rc.Files), tc.wantFiles)
			}
		})
	}
}

func TestNewSlackRawContent_RoundTrip(t *testing.T) {
	msg := goslack.Msg{
		Files: []goslack.File{
			{Name: "image.png", Mimetype: "image/png", Size: 12345},
		},
		Attachments: []goslack.Attachment{
			{Title: "PR #42", Text: "Fix the bug", Fallback: "PR #42 - Fix the bug"},
		},
	}
	rc := NewSlackRawContent(msg)
	raw := rc.AsSerializable()
	if raw == nil {
		t.Fatal("raw is nil")
	}

	// Simulate storage: marshal to JSON and unmarshal back.
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var restored map[string]any
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Verify files survived.
	files := restored["files"].([]any)
	if len(files) != 1 {
		t.Fatalf("files count = %d, want 1", len(files))
	}
	if files[0].(map[string]any)["name"] != "image.png" {
		t.Errorf("file name = %v, want image.png", files[0].(map[string]any)["name"])
	}

	// Verify attachments survived.
	attachments := restored["attachments"].([]any)
	if len(attachments) != 1 {
		t.Fatalf("attachments count = %d, want 1", len(attachments))
	}
	if attachments[0].(map[string]any)["title"] != "PR #42" {
		t.Errorf("attachment title = %v, want PR #42", attachments[0].(map[string]any)["title"])
	}
}
