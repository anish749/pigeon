package api

import (
	"github.com/anish749/pigeon/internal/platform/slack/slackerr"
)

// slackChannelNotFoundHint returns a human-friendly hint when a Slack API error
// is channel_not_found, otherwise returns an empty string.
func slackChannelNotFoundHint(err error) string {
	if slackerr.IsChannelNotFound(err) {
		return " — the bot may not be a member of this channel. " +
			"For private channels, ask someone to invite the bot. " +
			"For Slack Connect channels, ensure the bot is added to the shared channel."
	}
	return ""
}
