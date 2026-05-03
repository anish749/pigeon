package paths

import (
	"path/filepath"
	"strings"
)

// Classify inspects path and returns the typed DataFile value matching its
// shape, or nil if path does not match any known data-file shape under the
// pigeon data tree. The dispatch is purely lexical — no I/O — and operates
// on absolute or relative paths interchangeably, since every kind is
// distinguished by filename and parent-directory segments.
//
// This is the inverse of the typed constructors (ConversationDir.DateFile,
// GmailDir.DateFile, IdentityDir.PeopleFile, etc.): constructors build a
// typed path from structured info; Classify recovers the type from a string.
// Together they make the "what kind of file is this" question expressible
// without ad-hoc path inspection at every call site.
//
// Order of dispatch matters and is from most specific to least specific:
//   - drive-meta-YYYY-MM-DD.json (full-filename + extension match)
//   - account-level state files (MaintenanceFilename, SyncCursorsFilename,
//     pollMetricsFile, pendingDeletesFile, ConvMetaFilename)
//   - workstream router state (parent dir == WorkstreamSubdir)
//   - identity people file (parent dir == IdentitySubdir)
//   - Drive subtree: attachments → formula CSV → CSV → markdown → comments JSONL
//   - Linear per-issue logs (issue.jsonl / comments.jsonl, under linear platform)
//   - Jira per-issue logs (issue.jsonl / comments.jsonl, under jira platform)
//   - thread file (parent dir == ThreadsSubdir, filename != date)
//   - YYYY-MM-DD.jsonl, dispatched into Email/Calendar/Messaging by location
//
// A drive-meta filename whose date portion is malformed returns nil rather
// than an error — Classify is a pure dispatcher; callers needing the
// loud-on-malformed semantics should call ParseDriveMetaPath directly.
func Classify(path string) DataFile {
	base := filepath.Base(path)

	// 1. Drive-meta sidecar — drive-meta-YYYY-MM-DD.json.
	if meta, ok, err := ParseDriveMetaPath(path); ok && err == nil {
		return meta
	}

	// 2. Account-level state files (exact filename matches).
	switch base {
	case MaintenanceFilename:
		return MaintenanceFile(path)
	case SyncCursorsFilename:
		return SyncCursorsFile(path)
	case pollMetricsFile:
		return PollMetricsFile(path)
	case pendingDeletesFile:
		return PendingDeletesFile(path)
	case ConvMetaFilename:
		return ConvMetaFile(path)
	}

	parent := filepath.Base(filepath.Dir(path))

	// 3. Workstream router state: <root>/.workspaces/<name>/workstream/{file}.
	// Filename match plus parent-dir == WorkstreamSubdir distinguishes these
	// from arbitrary JSON files of the same name elsewhere in the tree.
	if parent == WorkstreamSubdir {
		switch base {
		case WorkstreamsFilename:
			return WorkstreamsFile(path)
		case WorkstreamProposalsFilename:
			return WorkstreamProposalsFile(path)
		}
	}

	// 4. Identity people file: <acct>/identity/people.jsonl.
	if parent == IdentitySubdir && base == PeopleFilename {
		return PeopleFile(path)
	}

	// 5. Drive subtree: anything under a gdrive segment.
	if pathHasSegment(path, GdriveSubdir) {
		if pathHasSegment(path, attachSubdir) {
			return AttachmentFile(path)
		}
		switch {
		case strings.HasSuffix(base, formulaCSVSuffix):
			return FormulaFile(path)
		case filepath.Ext(base) == CSVExt:
			return SheetFile(path)
		case filepath.Ext(base) == MarkdownExt:
			return TabFile(path)
		case base == commentsFile+FileExt:
			return CommentsFile(path)
		}
	}

	// 6. Linear per-issue logs:
	//   <root>/linear/<acct>/issues/<id>/issue.jsonl
	//   <root>/linear/<acct>/issues/<id>/comments.jsonl
	// Match by filename plus the linear platform segment so the same base
	// names under unrelated trees (e.g. Drive's comments.jsonl) keep their
	// existing classification.
	if pathHasSegment(path, linearPlatform) {
		switch base {
		case linearIssueFilename:
			return LinearIssueFile(path)
		case linearCommentsFilename:
			return LinearCommentsFile(path)
		}
	}

	// 7. Jira per-issue logs:
	//   <root>/jira/<acct>/<project>/issues/<KEY>/issue.jsonl
	//   <root>/jira/<acct>/<project>/issues/<KEY>/comments.jsonl
	// Same shape as Linear but a distinct platform segment so the two
	// platforms route to their own typed file kinds.
	if pathHasSegment(path, JiraPlatform) {
		switch base {
		case jiraIssueFilename:
			return JiraIssueFile(path)
		case jiraCommentsFilename:
			return JiraCommentsFile(path)
		}
	}

	// 8. Thread file: <conv>/threads/<ts>.jsonl. IsThreadFile already
	// excludes YYYY-MM-DD.jsonl so a conversation literally named "threads"
	// keeps its date children classified as messaging-date below.
	if IsThreadFile(path) {
		return ThreadFile(path)
	}

	// 9. Date-named JSONL — disambiguate by location segment.
	if IsDateFile(base) {
		switch {
		case pathHasSegment(path, GmailSubdir):
			return EmailDateFile(path)
		case pathHasSegment(path, GcalendarSubdir):
			return CalendarDateFile(path)
		default:
			return MessagingDateFile(path)
		}
	}

	return nil
}

// pathHasSegment reports whether any separator-delimited segment of path
// equals seg. Segment match avoids false positives where seg appears inside
// another path component (e.g. "gmailbackup" should not match "gmail").
func pathHasSegment(path, seg string) bool {
	sep := string(filepath.Separator)
	return strings.Contains(sep+path+sep, sep+seg+sep)
}
