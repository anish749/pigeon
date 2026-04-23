// Package slackerr provides predicates for classifying Slack API error
// responses. It is a leaf package imported by both the inbound listener and
// the outbound api layer so error-code checks stay in one place.
package slackerr

import (
	"errors"

	goslack "github.com/slack-go/slack"
)

// Is reports whether err is (or wraps) a Slack API error response with the
// given error code (e.g. "channel_not_found", "is_archived"). A plain error
// whose message happens to contain the code does not match.
func Is(err error, code string) bool {
	var slackErr goslack.SlackErrorResponse
	return errors.As(err, &slackErr) && slackErr.Err == code
}

// IsChannelNotFound reports whether err is (or wraps) a Slack
// "channel_not_found" API error response. This is the error Slack returns
// when the token used to call conversations.info (or similar) has no
// visibility into the channel — e.g. a user token querying a
// bot↔other-user DM.
func IsChannelNotFound(err error) bool {
	return Is(err, "channel_not_found")
}
