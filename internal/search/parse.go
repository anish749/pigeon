// Package search parses rg/grep output and produces structured search results.
package search

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/anish749/pigeon/internal/store/modelv1"
)

// Match is a message extracted from rg/grep output with its file location.
type Match struct {
	Platform     string
	Account      string
	Conversation string
	Date         string
	Msg          modelv1.MsgLine
}

// ParseGrepOutput extracts message-type events from rg/grep output.
// Each output line has the form: /path/to/file.txt:{"type":"msg",...}
// Only message events are returned; reactions, edits, deletes, and
// context separator lines are skipped. Lines that fail to parse are
// collected into the returned error, but successfully parsed matches
// are still returned.
func ParseGrepOutput(output []byte, searchDir string) ([]Match, error) {
	var matches []Match
	var errs []error
	for _, line := range bytes.Split(output, []byte("\n")) {
		if len(line) == 0 || bytes.Equal(line, []byte("--")) {
			continue
		}

		// rg/grep output: /path/to/file.txt:{"type":"msg",...}
		// Split on ":{"  — the colon is grep's delimiter, { starts the JSON.
		idx := bytes.Index(line, []byte(":{"))
		if idx < 0 {
			continue
		}
		filePart := string(line[:idx])
		jsonPart := line[idx+1:] // skip the ":", keep the "{"

		var envelope struct {
			Type modelv1.LineType `json:"type"`
		}
		if err := json.Unmarshal(jsonPart, &envelope); err != nil {
			errs = append(errs, fmt.Errorf("parse grep line: %w", err))
			continue
		}
		if envelope.Type != modelv1.LineMessage {
			continue
		}

		var msg modelv1.MsgLine
		if err := json.Unmarshal(jsonPart, &msg); err != nil {
			errs = append(errs, fmt.Errorf("parse msg line: %w", err))
			continue
		}

		platform, account, conversation, date := ParseFilePath(filePart, searchDir)
		matches = append(matches, Match{
			Platform:     platform,
			Account:      account,
			Conversation: conversation,
			Date:         date,
			Msg:          msg,
		})
	}
	return matches, errors.Join(errs...)
}

// ParseFilePath extracts platform/account/conversation/date from a grep
// output file path. The path is relative to searchDir. The trailing colon
// from grep output is stripped.
//
// Date files:   platform/account/conversation/YYYY-MM-DD.txt
// Thread files:  platform/account/conversation/threads/THREAD_TS.txt
//
// When searchDir already includes platform or account, those leading
// components are absent from the relative path.
func ParseFilePath(filePart, searchDir string) (platform, account, conversation, date string) {
	filePart = strings.TrimSuffix(strings.TrimSpace(filePart), ":")

	rel, err := filepath.Rel(searchDir, filePart)
	if err != nil {
		return
	}
	parts := strings.Split(rel, string(filepath.Separator))

	// Strip "threads" directory if present — thread files are
	// conversation/threads/TS.txt; we want the conversation, not "threads".
	// Remove the "threads" element so the rest of the logic works uniformly.
	for i, p := range parts {
		if p == "threads" {
			parts = append(parts[:i], parts[i+1:]...)
			break
		}
	}

	dateFile := parts[len(parts)-1]
	date = strings.TrimSuffix(dateFile, ".txt")

	switch len(parts) {
	case 4:
		platform, account, conversation = parts[0], parts[1], parts[2]
	case 3:
		account, conversation = parts[0], parts[1]
	case 2:
		conversation = parts[0]
	}
	return
}

