package gmail

import (
	"encoding/base64"
	"mime"
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
	// Single-part text/html: enmime doesn't populate env.HTML, but
	// parseRawMessage falls back to env.Root.Content.
	if parsed.html == "" {
		t.Error("html is empty, expected fallback to root part content")
	}
}

func TestParseRawMessage_PaddedBase64URL(t *testing.T) {
	// Regression: Gmail's API returns the `raw` field as padded base64url.
	// parseRawMessage previously used RawURLEncoding, which rejects `=`
	// padding and fails with "decode raw message: illegal base64 data ...".
	// The error byte offset points at the first `=` of the trailing padding.
	msg := "From: sender@example.com\r\n" +
		"To: recipient@example.com\r\n" +
		"Subject: Padded\r\n" +
		"Content-Type: text/plain\r\n" +
		"\r\n" +
		"Body!"

	// base64.URLEncoding pads to a multiple of 4; this fixture ends in `==`.
	raw := base64.URLEncoding.EncodeToString([]byte(msg))
	if got := raw[len(raw)-2:]; got != "==" {
		t.Fatalf("fixture should end in ==, got %q (full: %q)", got, raw)
	}

	parsed, err := parseRawMessage(raw)
	if err != nil {
		t.Fatalf("parseRawMessage rejected padded base64url: %v", err)
	}
	if parsed.subject != "Padded" {
		t.Errorf("subject = %q, want %q", parsed.subject, "Padded")
	}
	if parsed.text != "Body!" {
		t.Errorf("text = %q, want %q", parsed.text, "Body!")
	}
}

func TestParseRawMessage_MalformedSubPartContentType(t *testing.T) {
	// Regression: some bulk-mail senders generate attachments whose
	// Content-Type header contains literal shell error output (e.g.
	// "cannot open (No such file or directory)") because their template
	// pastes the stderr of a `file` invocation verbatim. Go's
	// mime.ParseMediaType rejects this with "expected slash after first
	// token", which previously caused enmime to drop the entire envelope
	// — losing the valid body text along with the bad attachment.
	//
	// The parser is configured with SkipMalformedParts so the body is
	// recovered and the broken part is reported via parsedMessage.warnings.
	raw := encode("From: sender@example.com\r\n" +
		"To: recipient@example.com\r\n" +
		"Subject: Broken attachment\r\n" +
		"Content-Type: multipart/mixed; boundary=outer\r\n" +
		"\r\n" +
		"--outer\r\n" +
		"Content-Type: text/plain\r\n" +
		"\r\n" +
		"Body still readable\r\n" +
		"--outer\r\n" +
		"Content-Type: cannot open (No such file or directory)\r\n" +
		"Content-Disposition: attachment; filename=\"doc.pdf\"\r\n" +
		"Content-Transfer-Encoding: base64\r\n" +
		"\r\n" +
		"JVBERi0xLjQK\r\n" +
		"--outer--\r\n")

	parsed, err := parseRawMessage(raw)
	if err != nil {
		t.Fatalf("parseRawMessage should recover from malformed sub-part: %v", err)
	}
	if parsed.subject != "Broken attachment" {
		t.Errorf("subject = %q, want %q", parsed.subject, "Broken attachment")
	}
	if parsed.text != "Body still readable" {
		t.Errorf("text = %q, want %q", parsed.text, "Body still readable")
	}
	if len(parsed.warnings) == 0 {
		t.Error("warnings is empty, expected the malformed part to be reported")
	}
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

func TestParseAddress_RFC2047_BEncoding(t *testing.T) {
	wantName := "Ålice Smïth"
	encoded := mime.BEncoding.Encode("utf-8", wantName)
	header := encoded + " <alice@example.com>"

	name, email := parseAddress(header)
	if name != wantName {
		t.Errorf("name = %q, want %q", name, wantName)
	}
	if email != "alice@example.com" {
		t.Errorf("email = %q, want %q", email, "alice@example.com")
	}
}

func TestParseAddress_RFC2047_QEncoding(t *testing.T) {
	wantName := "Müller"
	encoded := mime.QEncoding.Encode("utf-8", wantName)
	header := encoded + " <muller@example.com>"

	name, email := parseAddress(header)
	if name != wantName {
		t.Errorf("name = %q, want %q", name, wantName)
	}
	if email != "muller@example.com" {
		t.Errorf("email = %q, want %q", email, "muller@example.com")
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

// Regression: some mailers emit "Name <email >" with a trailing space inside
// the angle brackets. net/mail rejects this as "unclosed angle-addr".
func TestParseAddress_TrailingSpaceInAngleAddr(t *testing.T) {
	name, email := parseAddress("Sender Name <sender@example.com >")
	if name != "Sender Name" {
		t.Errorf("name = %q, want %q", name, "Sender Name")
	}
	if email != "sender@example.com" {
		t.Errorf("email = %q, want %q", email, "sender@example.com")
	}
}

// Regression: net/mail returns "mail: header not in message" for an empty
// string, so we must skip empty values rather than passing them to ParseAddressList.
func TestParseAddresses_EmptyValue(t *testing.T) {
	emails := parseAddresses([]string{""})
	if len(emails) != 0 {
		t.Errorf("emails = %v, want empty", emails)
	}
}

func TestSanitizeAddrHeader(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"no angle brackets", "no angle brackets"},
		{"Name <user@example.com>", "Name <user@example.com>"},
		{"Name <user@example.com >", "Name <user@example.com>"},
		{"Name < user@example.com >", "Name <user@example.com>"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := sanitizeAddrHeader(tc.in); got != tc.want {
			t.Errorf("sanitizeAddrHeader(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
