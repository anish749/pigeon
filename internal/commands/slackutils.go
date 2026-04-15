package commands

import (
	"fmt"

	"github.com/anish749/pigeon/internal/read"
	"github.com/anish749/pigeon/internal/search"
)

// messageExists reports whether a message with the given ID exists anywhere
// under accountDir. It greps all JSONL files and verifies a message-type line
// with a matching ID is found. Works across channels, DMs, and MPDMs.
func messageExists(accountDir, messageID string) (bool, error) {
	output, err := read.Grep(accountDir, read.GrepOpts{
		Query:        messageID,
		FixedStrings: true,
		JSON:         true,
	})
	if err != nil {
		return false, fmt.Errorf("grep %s: %w", accountDir, err)
	}
	if output == nil {
		return false, nil
	}

	// ParseGrepOutput returns partial results + error (unparseable lines are
	// collected into err but valid matches are still returned).
	matches, parseErr := search.ParseGrepOutput(output, accountDir)
	for _, m := range matches {
		if id, ok := m.Line.ID(); ok && id == messageID {
			return true, nil
		}
	}
	if parseErr != nil {
		return false, fmt.Errorf("parse grep output: %w", parseErr)
	}
	return false, nil
}
