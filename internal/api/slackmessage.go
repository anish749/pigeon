package api

import (
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
