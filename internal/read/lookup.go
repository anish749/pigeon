package read

import (
	"strings"

	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store/modelv1"
)

// LookupMessage searches conv (date JSONL and thread files under the
// conversation directory) for a message line with msgID using ripgrep.
// Returns nil if not found or on error.
func LookupMessage(conv paths.ConversationDir, msgID string) *modelv1.MsgLine {
	out, err := Grep(conv.Path(), GrepOpts{
		Query:        msgID,
		FixedStrings: true,
		NoFilename:   true,
	})
	if err != nil || len(out) == 0 {
		return nil
	}

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parsed, err := modelv1.Parse(line)
		if err != nil {
			continue
		}
		if parsed.Type == modelv1.LineMessage && parsed.Msg != nil && parsed.Msg.ID == msgID {
			return parsed.Msg
		}
	}
	return nil
}
