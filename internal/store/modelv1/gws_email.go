package modelv1

import "time"

// EmailLine represents a single Gmail message in JSONL format.
type EmailLine struct {
	ID       string            `json:"id"`               // Gmail message ID
	ThreadID string            `json:"threadId"`         // Gmail thread ID
	Ts       time.Time         `json:"ts"`               // when Gmail received it
	From     string            `json:"from"`             // sender email
	FromName string            `json:"fromName"`         // sender display name
	To       []string          `json:"to"`               // recipient emails
	CC       []string          `json:"cc,omitempty"`     // CC emails
	Subject  string            `json:"subject"`          // email subject
	Labels   []string          `json:"labels"`           // Gmail labels
	Snippet  string            `json:"snippet"`          // text preview
	Text     string            `json:"text"`             // plain text body (from text/plain part)
	HTML     string            `json:"html,omitempty"`   // raw HTML body (from text/html part or single-part HTML message)
	Attach   []EmailAttachment `json:"attach,omitempty"` // attachments
}

// EmailAttachment represents a file attached to an email.
type EmailAttachment struct {
	ID   string `json:"id"`   // Gmail attachment/part ID
	Type string `json:"type"` // MIME type
	Name string `json:"name"` // filename
}
