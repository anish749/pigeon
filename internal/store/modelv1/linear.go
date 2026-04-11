package modelv1

// LinearIssue holds the raw CLI JSON (Serialized) and a minimal parsed
// struct (Runtime) for dedup and cursor extraction. Follows the same
// dual-representation pattern as CalendarEvent and DriveComment.
type LinearIssue struct {
	Runtime    LinearIssueRuntime
	Serialized map[string]any
}

// LinearIssueRuntime holds only the fields pigeon needs for dedup (ID),
// cursor tracking (UpdatedAt), and file routing (Identifier). Everything
// else lives in Serialized and round-trips through disk unchanged.
type LinearIssueRuntime struct {
	ID         string `json:"id"`
	Identifier string `json:"identifier"`
	UpdatedAt  string `json:"updatedAt"`
}

// LinearComment holds the raw CLI JSON (Serialized) and a minimal parsed
// struct (Runtime) for dedup. The same dual-representation pattern as
// DriveComment, but the Runtime is a small hand-written struct because
// the linear CLI returns arbitrary JSON, not a typed Go struct.
type LinearComment struct {
	Runtime    LinearCommentRuntime
	Serialized map[string]any
}

// LinearCommentRuntime holds only the fields pigeon needs for dedup (ID).
type LinearCommentRuntime struct {
	ID        string `json:"id"`
	CreatedAt string `json:"createdAt"`
}
