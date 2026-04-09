package model

import "time"

// CommentLine represents a Google Drive comment in JSONL format.
type CommentLine struct {
	ID       string    `json:"id"`               // Drive comment ID
	Ts       time.Time `json:"ts"`               // created time
	Author   string    `json:"author"`           // display name
	Content  string    `json:"content"`          // comment text
	Anchor   string    `json:"anchor,omitempty"` // highlighted/quoted text
	Resolved bool      `json:"resolved"`         // thread resolved state
}

// ReplyLine represents a reply to a Google Drive comment in JSONL format.
type ReplyLine struct {
	ID        string    `json:"id"`               // Drive reply ID
	CommentID string    `json:"commentId"`        // parent comment ID
	Ts        time.Time `json:"ts"`               // created time
	Author    string    `json:"author"`           // display name
	Content   string    `json:"content"`          // reply text
	Action    string    `json:"action,omitempty"` // "resolve" or "reopen"
}
