package gmail

import (
	"testing"
)

func TestExtractBody_SimplePlainText(t *testing.T) {
	payload := gmailPayload{
		MimeType: "text/plain",
		Body:     gmailBody{Data: "SGVsbG8gd29ybGQ"}, // "Hello world"
	}
	got := ExtractBody(payload)
	if got != "Hello world" {
		t.Errorf("ExtractBody = %q, want %q", got, "Hello world")
	}
}

func TestExtractBody_MultipartAlternative(t *testing.T) {
	payload := gmailPayload{
		MimeType: "multipart/alternative",
		Parts: []gmailPayload{
			{
				MimeType: "text/plain",
				Body:     gmailBody{Data: "UGxhaW4gdGV4dCBib2R5"}, // "Plain text body"
			},
			{
				MimeType: "text/html",
				Body:     gmailBody{Data: "PHA-SGVsbG8gPGI-d29ybGQ8L2I-PC9wPg"}, // "<p>Hello <b>world</b></p>"
			},
		},
	}
	got := ExtractBody(payload)
	if got != "Plain text body" {
		t.Errorf("ExtractBody = %q, want %q", got, "Plain text body")
	}
}

func TestExtractBody_HTMLOnly(t *testing.T) {
	payload := gmailPayload{
		MimeType: "text/html",
		Body:     gmailBody{Data: "PHA-SGVsbG8gPGI-d29ybGQ8L2I-PC9wPg"}, // "<p>Hello <b>world</b></p>"
	}
	got := ExtractBody(payload)
	if got != "Hello world" {
		t.Errorf("ExtractBody = %q, want %q", got, "Hello world")
	}
}

func TestExtractBody_NestedMultipart(t *testing.T) {
	payload := gmailPayload{
		MimeType: "multipart/mixed",
		Parts: []gmailPayload{
			{
				MimeType: "multipart/alternative",
				Parts: []gmailPayload{
					{
						MimeType: "text/plain",
						Body:     gmailBody{Data: "TmVzdGVkIHBsYWluIHRleHQ"}, // "Nested plain text"
					},
					{
						MimeType: "text/html",
						Body:     gmailBody{Data: "PHA-SGVsbG8gPGI-d29ybGQ8L2I-PC9wPg"},
					},
				},
			},
			{
				MimeType: "application/pdf",
				Filename: "report.pdf",
				Body:     gmailBody{AttachmentID: "att-1", Size: 1024},
			},
		},
	}
	got := ExtractBody(payload)
	if got != "Nested plain text" {
		t.Errorf("ExtractBody = %q, want %q", got, "Nested plain text")
	}
}

func TestExtractAttachments(t *testing.T) {
	payload := gmailPayload{
		MimeType: "multipart/mixed",
		Parts: []gmailPayload{
			{
				MimeType: "text/plain",
				Body:     gmailBody{Data: "SGVsbG8gd29ybGQ"},
			},
			{
				MimeType: "application/pdf",
				Filename: "report.pdf",
				Body:     gmailBody{AttachmentID: "att-1", Size: 1024},
			},
			{
				MimeType: "image/png",
				Filename: "screenshot.png",
				Body:     gmailBody{AttachmentID: "att-2", Size: 2048},
			},
		},
	}
	attachments := ExtractAttachments(payload)
	if len(attachments) != 2 {
		t.Fatalf("ExtractAttachments returned %d attachments, want 2", len(attachments))
	}

	if attachments[0].ID != "att-1" || attachments[0].Type != "application/pdf" || attachments[0].Name != "report.pdf" {
		t.Errorf("attachment[0] = %+v, want {ID:att-1 Type:application/pdf Name:report.pdf}", attachments[0])
	}
	if attachments[1].ID != "att-2" || attachments[1].Type != "image/png" || attachments[1].Name != "screenshot.png" {
		t.Errorf("attachment[1] = %+v, want {ID:att-2 Type:image/png Name:screenshot.png}", attachments[1])
	}
}

func TestExtractAttachments_NoAttachments(t *testing.T) {
	payload := gmailPayload{
		MimeType: "text/plain",
		Body:     gmailBody{Data: "SGVsbG8gd29ybGQ"},
	}
	attachments := ExtractAttachments(payload)
	if attachments != nil {
		t.Errorf("ExtractAttachments = %v, want nil", attachments)
	}
}

func TestDecodeBase64URL(t *testing.T) {
	// "Hello world" in base64url without padding
	got, err := decodeBase64URL("SGVsbG8gd29ybGQ")
	if err != nil {
		t.Fatalf("decodeBase64URL error: %v", err)
	}
	if got != "Hello world" {
		t.Errorf("decodeBase64URL = %q, want %q", got, "Hello world")
	}
}

func TestStripHTMLTags(t *testing.T) {
	tests := []struct {
		name string
		html string
		want string
	}{
		{
			name: "simple tags",
			html: "<p>Hello <b>world</b></p>",
			want: "Hello world",
		},
		{
			name: "br tags",
			html: "line1<br>line2<br/>line3",
			want: "line1 line2 line3",
		},
		{
			name: "nested tags",
			html: "<div><p>Hello</p><p>World</p></div>",
			want: "Hello World",
		},
		{
			name: "no tags",
			html: "plain text",
			want: "plain text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripHTMLTags(tt.html)
			if got != tt.want {
				t.Errorf("stripHTMLTags(%q) = %q, want %q", tt.html, got, tt.want)
			}
		})
	}
}
