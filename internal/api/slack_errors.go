package api

import (
	"errors"

	goslack "github.com/slack-go/slack"
)

// slackChannelNotFoundHint returns a human-friendly hint when a Slack API error
// is channel_not_found, otherwise returns an empty string.
func slackChannelNotFoundHint(err error) string {
	var slackErr goslack.SlackErrorResponse
	if errors.As(err, &slackErr) && slackErr.Err == "channel_not_found" {
		return " — the bot may not be a member of this channel. " +
			"For private channels, ask someone to invite the bot. " +
			"For Slack Connect channels, ensure the bot is added to the shared channel."
	}
	return ""
}
