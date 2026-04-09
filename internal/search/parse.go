// Package search parses rg/grep output and produces structured search results.
package search

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store/modelv1"
)

// Match is a message extracted from rg/grep output with its file location.
type Match struct {
	Platform     string
	Account      string
	Conversation string
	Date         string // date filename (YYYY-MM-DD) or thread timestamp
	Thread       bool   // true if match came from a thread file
	FilePath     string // absolute path to the source file
	Msg          modelv1.MsgLine
}

// rgJSONLine represents a single line of rg --json output.
// Only "match" and "context" types carry data we need.
type rgJSONLine struct {
	Type string `json:"type"`
	Data struct {
		Path  struct{ Text string } `json:"path"`
		Lines struct{ Text string } `json:"lines"`
	} `json:"data"`
}

// ParseGrepOutput extracts message-type events from rg --json output.
// The input must be produced by rg with the --json flag, which emits
// structured JSON with path and content as separate fields — no
// ambiguous delimiter parsing needed.
//
// Only message events are returned; reactions, edits, deletes, and
// non-match/context lines are skipped. Lines that fail to parse are
// collected into the returned error, but successfully parsed matches
// are still returned.
func ParseGrepOutput(output []byte, searchDir string) ([]Match, error) {
	var matches []Match
	var errs []error
	for _, line := range bytes.Split(output, []byte("\n")) {
		if len(line) == 0 {
			continue
		}

		var rg rgJSONLine
		if err := json.Unmarshal(line, &rg); err != nil {
			errs = append(errs, fmt.Errorf("parse rg json: %w", err))
			continue
		}
		if rg.Type != "match" && rg.Type != "context" {
			continue
		}

		content := strings.TrimRight(rg.Data.Lines.Text, "\n")
		var envelope struct {
			Type modelv1.LineType `json:"type"`
		}
		if err := json.Unmarshal([]byte(content), &envelope); err != nil {
			errs = append(errs, fmt.Errorf("parse grep line: %w", err))
			continue
		}
		if envelope.Type != modelv1.LineMessage {
			continue
		}

		var msg modelv1.MsgLine
		if err := json.Unmarshal([]byte(content), &msg); err != nil {
			errs = append(errs, fmt.Errorf("parse msg line: %w", err))
			continue
		}

		platform, account, conversation, date, thread, pathErr := ParseFilePath(rg.Data.Path.Text, searchDir)
		if pathErr != nil {
			errs = append(errs, pathErr)
			continue
		}
		matches = append(matches, Match{
			Platform:     platform,
			Account:      account,
			Conversation: conversation,
			Date:         date,
			Thread:       thread,
			FilePath:     rg.Data.Path.Text,
			Msg:          msg,
		})
	}
	return matches, errors.Join(errs...)
}

// FilterThreadsBySince drops matches from thread files where no message
// in that thread falls within the since window. If any message in a thread
// is recent, all matches from that thread are kept (preserving context).
// Non-thread matches are always kept (date files are already filtered by
// filename at the rg/grep level).
func FilterThreadsBySince(matches []Match, since time.Duration) []Match {
	cutoff := time.Now().Add(-since)

	// First pass: find which thread files have at least one recent message.
	type threadKey struct {
		platform, account, conversation, date string
	}
	alive := make(map[threadKey]bool)
	for _, m := range matches {
		if !m.Thread {
			continue
		}
		if !m.Msg.Ts.Before(cutoff) {
			k := threadKey{m.Platform, m.Account, m.Conversation, m.Date}
			alive[k] = true
		}
	}

	// Second pass: keep non-thread matches and alive thread matches.
	var out []Match
	for _, m := range matches {
		if !m.Thread {
			out = append(out, m)
			continue
		}
		k := threadKey{m.Platform, m.Account, m.Conversation, m.Date}
		if alive[k] {
			out = append(out, m)
		}
	}
	return out
}

// ParseFilePath extracts platform/account/conversation/date from a file
// path. The path is relative to searchDir.
//
// Date files:   platform/account/conversation/YYYY-MM-DD.jsonl
// Thread files:  platform/account/conversation/threads/THREAD_TS.jsonl
//
// When searchDir already includes platform or account, those leading
// components are absent from the relative path.
func ParseFilePath(filePart, searchDir string) (platform, account, conversation, date string, thread bool, err error) {
	filePart = strings.TrimSuffix(strings.TrimSpace(filePart), ":")

	rel, err := filepath.Rel(searchDir, filePart)
	if err != nil {
		return "", "", "", "", false, fmt.Errorf("parse file path: %w", err)
	}
	parts := strings.Split(rel, string(filepath.Separator))

	// A real thread file lives at conversation/threads/TS.jsonl. A
	// conversation literally named "threads" has YYYY-MM-DD.jsonl
	// children under its own path, which must NOT be treated as thread
	// files — strip the "threads" component only in the real thread case.
	if paths.IsThreadFile(filePart) {
		thread = true
		parts = parts[:len(parts)-2]
		parts = append(parts, filepath.Base(filePart))
	}

	dateFile := parts[len(parts)-1]
	date = strings.TrimSuffix(dateFile, paths.FileExt)

	switch len(parts) {
	case 4:
		platform, account, conversation = parts[0], parts[1], parts[2]
	case 3:
		account, conversation = parts[0], parts[1]
	case 2:
		conversation = parts[0]
	default:
		return "", "", "", "", false, fmt.Errorf("parse file path: unexpected depth %d in %q", len(parts), rel)
	}
	return platform, account, conversation, date, thread, nil
}
