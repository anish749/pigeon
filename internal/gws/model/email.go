package model

import "time"

// EmailLine represents a single Gmail message in JSONL format.
type EmailLine struct {
	Type     string            `json:"type"`             // always "email"
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
	Text     string            `json:"text"`             // plain text body
	Attach   []EmailAttachment `json:"attach,omitempty"` // attachments
}

// EmailAttachment represents a file attached to an email.
type EmailAttachment struct {
	ID   string `json:"id"`   // Gmail attachment/part ID
	Type string `json:"type"` // MIME type
	Name string `json:"name"` // filename
}

// EmailDeleteLine records the deletion of a Gmail message.
type EmailDeleteLine struct {
	Type string    `json:"type"` // always "email-delete"
	ID   string    `json:"id"`   // target message ID
	Ts   time.Time `json:"ts"`   // when observed
}
