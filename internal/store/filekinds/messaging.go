package filekinds

import (
	"path/filepath"
	"time"

	"github.com/anish749/pigeon/internal/paths"
)

// messagingDateKind matches slack and whatsapp date files:
//
//	<root>/<platform>/<account>/<conversation>/YYYY-MM-DD.jsonl
//
// Gmail and calendar also use date filenames but live under GWS service
// subdirs and are handled by their own kinds.
type messagingDateKind struct{}

func (messagingDateKind) Name() string { return "messaging-date" }

func (messagingDateKind) Match(path string) bool {
	if !paths.IsDateFile(filepath.Base(path)) {
		return false
	}
	if paths.IsThreadFile(path) {
		return false
	}
	for _, svc := range paths.GWSServices {
		if pathHasSegment(path, svc) {
			return false
		}
	}
	return true
}

func (messagingDateKind) LatestTs(path string) (time.Time, error) {
	return scanLatestTs(path, "ts")
}

// threadFileKind matches thread files:
//
//	<root>/<platform>/<account>/<conversation>/threads/<ts>.jsonl
//
// The file's name is the parent message ts; "latest activity" is the newest
// ts across the parent and all replies (plus any context lines, which scan
// harmlessly to an older ts and lose to the newest reply).
type threadFileKind struct{}

func (threadFileKind) Name() string { return "thread" }

func (threadFileKind) Match(path string) bool {
	return paths.IsThreadFile(path)
}

func (threadFileKind) LatestTs(path string) (time.Time, error) {
	return scanLatestTs(path, "ts")
}
