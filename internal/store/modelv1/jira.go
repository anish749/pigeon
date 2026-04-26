package modelv1

import jira "github.com/ankitpokhrel/jira-cli/pkg/jira"

// JiraIssue holds the raw HTTP response (Serialized) and the typed
// pkg/jira.Issue (Runtime) for dedup, cursor extraction, and file routing.
//
// pkg/jira.Issue carries every field pigeon needs for in-process logic:
// Key (file routing), Fields.Updated (cursor), and Fields.Status / Assignee /
// Priority / etc. (formatting). The numeric `id` used as the dedup key is
// NOT modeled by pkg/jira.Issue — it lives only in the raw response and is
// extracted from the Serialized map at write time.
//
// Same dual-representation pattern as CalendarEvent (gws_event.go).
type JiraIssue struct {
	Runtime    jira.Issue
	Serialized map[string]any
}

// JiraComment is a single entry from fields.comment.comments[]. pkg/jira
// does not expose a named comment type — IssueFields.Comment.Comments is
// declared as an anonymous struct in pkg/jira/types.go (which means we
// cannot import it as a Go type) — so the Runtime is a small local struct.
//
// IssueKey is injected by the poller (it is NOT in the raw API output) so
// each comment line self-identifies its parent. This avoids the Linear
// comment shape, where comments have no parent identifier and require the
// reader to know the file path to know which issue a comment belongs to.
type JiraComment struct {
	Runtime    JiraCommentRuntime
	Serialized map[string]any
}

// JiraCommentRuntime holds the fields pigeon uses for dedup (ID), parent
// routing (IssueKey), and ordering (Created, Updated). All other comment
// fields (author, body, self, jsdPublic, …) live in Serialized and
// round-trip through disk unchanged.
type JiraCommentRuntime struct {
	ID       string `json:"id"`
	IssueKey string `json:"issueKey"`
	Created  string `json:"created"`
	Updated  string `json:"updated"`
}
