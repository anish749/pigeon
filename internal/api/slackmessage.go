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
// <@>, which Slack would render as literal text.
func ValidateSlackMessage(msg string) error {
	if strings.Contains(msg, "<@>") {
		return fmt.Errorf("message contains an empty mention <@> — a mention needs a user ID, e.g. <@U012ABC3DE>")
	}
	return nil
}
