package api

import (
	"fmt"
	"strings"

	"github.com/anish749/pigeon/internal/utils/mrkdwn"
)

// ResolveSlackMessage prepares a raw message for Slack delivery:
//   - converts standard Markdown to Slack mrkdwn format
//   - undoes shell escaping (\! → !)
func ResolveSlackMessage(raw string) string {
	msg := mrkdwn.ToSlackMarkdown(raw)
	msg = strings.ReplaceAll(msg, `\!`, "!")
	return msg
}

// ValidateSlackMessage rejects outbound text containing an empty mention
// <@> — the artifact of an inline identity lookup that produced no output
// (e.g. an ambiguous $(pigeon whois --id) substitution).
func ValidateSlackMessage(msg string) error {
	if strings.Contains(msg, "<@>") {
		return fmt.Errorf("message contains an empty mention <@> — an inline identity lookup (e.g. $(pigeon whois --id)) produced no output")
	}
	return nil
}
