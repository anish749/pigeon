// Package search parses rg/grep output and produces structured search results.
package search

import (
	"bytes"
	"encoding/json"
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
// context separator lines are skipped.
func ParseGrepOutput(output []byte, searchDir string) []Match {
	var matches []Match
	for _, line := range bytes.Split(output, []byte("\n")) {
		if len(line) == 0 || bytes.Equal(line, []byte("--")) {
			continue
		}

		idx := bytes.IndexByte(line, '{')
		if idx < 0 {
			continue
		}
		filePart := string(line[:idx])
		jsonPart := line[idx:]

		var envelope struct {
			Type modelv1.LineType `json:"type"`
		}
		if err := json.Unmarshal(jsonPart, &envelope); err != nil {
			continue
		}
		if envelope.Type != modelv1.LineMessage {
			continue
		}

		var msg modelv1.MsgLine
		if err := json.Unmarshal(jsonPart, &msg); err != nil {
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
	return matches
}

// ParseFilePath extracts platform/account/conversation/date from a grep
// output file path. The path is relative to searchDir. The trailing colon
// from grep output is stripped.
//
// Depending on how deep searchDir is, the relative path has different depths:
//   - 4 parts: platform/account/conversation/date.txt  (searched from data root)
//   - 3 parts: account/conversation/date.txt           (searched from platform dir)
//   - 2 parts: conversation/date.txt                   (searched from account dir)
func ParseFilePath(filePart, searchDir string) (platform, account, conversation, date string) {
	filePart = strings.TrimSuffix(strings.TrimSpace(filePart), ":")

	rel, err := filepath.Rel(searchDir, filePart)
	if err != nil {
		return
	}
	parts := strings.Split(rel, string(filepath.Separator))

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

