package gmail

import (
	"encoding/base64"
	"fmt"
	"io"
	"strings"

	"golang.org/x/net/html"

	"github.com/anish749/pigeon/internal/gws/model"
)

// gmailPayload represents a MIME part in the Gmail API response.
type gmailPayload struct {
	MimeType string         `json:"mimeType"`
	Filename string         `json:"filename"`
	Headers  []gmailHeader  `json:"headers"`
	Body     gmailBody      `json:"body"`
	Parts    []gmailPayload `json:"parts"` // recursive MIME tree
}

type gmailHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type gmailBody struct {
	Data         string `json:"data"`         // base64url encoded
	Size         int    `json:"size"`
	AttachmentID string `json:"attachmentId"` // present for attachments
}

// ExtractBody walks the MIME tree to find the best text content.
// Prefers text/plain, falls back to text/html (with tags stripped).
func ExtractBody(payload gmailPayload) (string, error) {
	plain, err := findBody(payload, "text/plain")
	if err != nil {
		return "", fmt.Errorf("extract text/plain body: %w", err)
	}
	if plain != "" {
		return plain, nil
	}

	html, err := findBody(payload, "text/html")
	if err != nil {
		return "", fmt.Errorf("extract text/html body: %w", err)
	}
	if html != "" {
		return stripHTMLTags(html), nil
	}
	return "", nil
}

// findBody searches the MIME tree depth-first for a part matching mimeType
// and returns its decoded body content.
func findBody(payload gmailPayload, mimeType string) (string, error) {
	if len(payload.Parts) == 0 {
		if strings.EqualFold(payload.MimeType, mimeType) && payload.Body.Data != "" {
			return decodeBase64URL(payload.Body.Data)
		}
		return "", nil
	}

	for _, part := range payload.Parts {
		body, err := findBody(part, mimeType)
		if err != nil {
			return "", err
		}
		if body != "" {
			return body, nil
		}
	}
	return "", nil
}

// ExtractAttachments collects attachment metadata from the MIME tree.
func ExtractAttachments(payload gmailPayload) []model.EmailAttachment {
	var attachments []model.EmailAttachment
	collectAttachments(payload, &attachments)
	if len(attachments) == 0 {
		return nil
	}
	return attachments
}

func collectAttachments(payload gmailPayload, out *[]model.EmailAttachment) {
	if payload.Body.AttachmentID != "" || payload.Filename != "" {
		*out = append(*out, model.EmailAttachment{
			ID:   payload.Body.AttachmentID,
			Type: payload.MimeType,
			Name: payload.Filename,
		})
	}
	for _, part := range payload.Parts {
		collectAttachments(part, out)
	}
}

// decodeBase64URL decodes Gmail's base64url-encoded body data.
// Gmail uses URL-safe base64 without padding (RFC 4648 section 5).
func decodeBase64URL(data string) (string, error) {
	b, err := base64.RawURLEncoding.DecodeString(data)
	if err != nil {
		return "", fmt.Errorf("decode base64url: %w", err)
	}
	return string(b), nil
}

// stripHTMLTags extracts text content from HTML using golang.org/x/net/html
// tokenizer. Skips script and style elements.
func stripHTMLTags(s string) string {
	tokenizer := html.NewTokenizer(strings.NewReader(s))
	var sb strings.Builder
	skip := false
	for {
		tt := tokenizer.Next()
		switch tt {
		case html.ErrorToken:
			if tokenizer.Err() == io.EOF {
				return strings.TrimSpace(strings.Join(strings.Fields(sb.String()), " "))
			}
			return strings.TrimSpace(strings.Join(strings.Fields(sb.String()), " "))
		case html.StartTagToken:
			tn, _ := tokenizer.TagName()
			tag := string(tn)
			if tag == "script" || tag == "style" {
				skip = true
			}
		case html.EndTagToken:
			tn, _ := tokenizer.TagName()
			tag := string(tn)
			if tag == "script" || tag == "style" {
				skip = false
			}
		case html.TextToken:
			if !skip {
				sb.WriteString(tokenizer.Token().Data)
				sb.WriteByte(' ')
			}
		}
	}
}
