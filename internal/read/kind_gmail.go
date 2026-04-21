package read

import (
	"path/filepath"
	"time"

	"github.com/anish749/pigeon/internal/paths"
)

// emailDateKind matches gmail date files:
//
//	<root>/gws/<account>/gmail/YYYY-MM-DD.jsonl
//
// Each line serialises an EmailLine with a "ts" field — the same scan used
// by messaging kinds applies.
type emailDateKind struct{}

func (emailDateKind) Name() string { return "email-date" }

func (emailDateKind) Match(path string) bool {
	if !paths.IsDateFile(filepath.Base(path)) {
		return false
	}
	return pathHasSegment(path, paths.GmailSubdir)
}

func (emailDateKind) LatestTs(path string) (time.Time, error) {
	return scanTsField(path)
}
