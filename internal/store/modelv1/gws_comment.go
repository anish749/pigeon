package modelv1

import (
	drive "google.golang.org/api/drive/v3"
)

// DriveComment holds two representations of a single Drive comment: a
// typed view for in-process code and a raw map that is the source of truth
// for disk storage.
//
// Runtime is the typed drive.Comment used by dedup and any code that needs
// typed field access. Serialized is a JSON-shaped map that MarshalGWS writes
// verbatim to disk — it preserves every field the API returned, including
// nested replies, even ones the generated SDK types don't know about.
//
// Only Serialized is persisted. Mutations to Runtime are not reflected on
// disk unless they're also pushed into Serialized, so treat Runtime as a
// read-only view of the comment.
type DriveComment struct {
	Runtime    drive.Comment
	Serialized map[string]any
}
