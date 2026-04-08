package gmail

import (
	"encoding/base64"
	"testing"
)

func encode(s string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(s))
}

func TestParseRawMessage_PlainText(t *testing.T) {
	raw := encode("From: Alice <alice@example.com>\r\n" +
		"To: bob@example.com\r\n" +
		"Subject: Hello\r\n" +
		"Content-Type: text/plain\r\n" +
		"\r\n" +
		"Hello world")

	parsed, err := parseRawMessage(raw)
	if err != nil {
		t.Fatalf("parseRawMessage: %v", err)
	}
	if parsed.fromName != "Alice" {
		t.Errorf("fromName = %q, want %q", parsed.fromName, "Alice")
	}
	if parsed.fromAddr != "alice@example.com" {
		t.Errorf("fromAddr = %q, want %q", parsed.fromAddr, "alice@example.com")
	}
	if len(parsed.to) != 1 || parsed.to[0] != "bob@example.com" {
		t.Errorf("to = %v, want [bob@example.com]", parsed.to)
	}
	if parsed.subject != "Hello" {
		t.Errorf("subject = %q, want %q", parsed.subject, "Hello")
	}
	if parsed.text != "Hello world" {
		t.Errorf("text = %q, want %q", parsed.text, "Hello world")
	}
	if len(parsed.attachments) != 0 {
		t.Errorf("attachments = %v, want empty", parsed.attachments)
	}
}

func TestParseRawMessage_Multipart(t *testing.T) {
	raw := encode("From: sender@example.com\r\n" +
		"To: a@example.com, b@example.com\r\n" +
		"Cc: c@example.com\r\n" +
		"Subject: Multi\r\n" +
		"Content-Type: multipart/alternative; boundary=boundary42\r\n" +
		"\r\n" +
		"--boundary42\r\n" +
		"Content-Type: text/plain\r\n" +
		"\r\n" +
		"Plain body\r\n" +
		"--boundary42\r\n" +
		"Content-Type: text/html\r\n" +
		"\r\n" +
		"<p>HTML body</p>\r\n" +
		"--boundary42--\r\n")

	parsed, err := parseRawMessage(raw)
	if err != nil {
		t.Fatalf("parseRawMessage: %v", err)
	}
	if parsed.text != "Plain body" {
		t.Errorf("text = %q, want %q", parsed.text, "Plain body")
	}
	if parsed.html == "" {
		t.Error("html is empty, expected raw HTML from multipart/alternative")
	}
	if len(parsed.to) != 2 {
		t.Errorf("to = %v, want 2 addresses", parsed.to)
	}
	if len(parsed.cc) != 1 || parsed.cc[0] != "c@example.com" {
		t.Errorf("cc = %v, want [c@example.com]", parsed.cc)
	}
}

func TestParseRawMessage_HTMLOnly(t *testing.T) {
	raw := encode("From: sender@example.com\r\n" +
		"Subject: HTML\r\n" +
		"Content-Type: text/html\r\n" +
		"\r\n" +
		"<p>Hello <b>world</b></p>\r\n")

	parsed, err := parseRawMessage(raw)
	if err != nil {
		t.Fatalf("parseRawMessage: %v", err)
	}
	// enmime converts HTML→text automatically, so text is populated.
	if parsed.text == "" {
		t.Error("text is empty, expected enmime HTML→text conversion")
	}
	// Single-part text/html: enmime doesn't populate HTML separately.
	// HTML is only set for multipart messages with an explicit text/html part.
}

func TestParseRawMessage_WithAttachment(t *testing.T) {
	raw := encode("From: sender@example.com\r\n" +
		"Subject: Attachment\r\n" +
		"Content-Type: multipart/mixed; boundary=mixbound\r\n" +
		"\r\n" +
		"--mixbound\r\n" +
		"Content-Type: text/plain\r\n" +
		"\r\n" +
		"See attached\r\n" +
		"--mixbound\r\n" +
		"Content-Type: application/pdf; name=\"report.pdf\"\r\n" +
		"Content-Disposition: attachment; filename=\"report.pdf\"\r\n" +
		"Content-Transfer-Encoding: base64\r\n" +
		"\r\n" +
		"JVBERi0xLjQK\r\n" +
		"--mixbound--\r\n")

	parsed, err := parseRawMessage(raw)
	if err != nil {
		t.Fatalf("parseRawMessage: %v", err)
	}
	if parsed.text != "See attached" {
		t.Errorf("text = %q, want %q", parsed.text, "See attached")
	}
	if len(parsed.attachments) != 1 {
		t.Fatalf("attachments count = %d, want 1", len(parsed.attachments))
	}
	if parsed.attachments[0].Name != "report.pdf" {
		t.Errorf("attachment name = %q, want %q", parsed.attachments[0].Name, "report.pdf")
	}
	if parsed.attachments[0].Type != "application/pdf" {
		t.Errorf("attachment type = %q, want %q", parsed.attachments[0].Type, "application/pdf")
	}
}

func TestParseAddress(t *testing.T) {
	name, email := parseAddress("Alice Smith <alice@example.com>")
	if name != "Alice Smith" {
		t.Errorf("name = %q, want %q", name, "Alice Smith")
	}
	if email != "alice@example.com" {
		t.Errorf("email = %q, want %q", email, "alice@example.com")
	}
}

func TestParseAddresses(t *testing.T) {
	emails := parseAddresses([]string{"a@example.com, b@example.com"})
	if len(emails) != 2 {
		t.Fatalf("len = %d, want 2", len(emails))
	}
	if emails[0] != "a@example.com" || emails[1] != "b@example.com" {
		t.Errorf("emails = %v", emails)
	}
}
