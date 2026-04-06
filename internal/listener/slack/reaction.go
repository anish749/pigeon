package slack

import (
	"context"
	"errors"

	goslack "github.com/slack-go/slack"

	"github.com/anish749/pigeon/internal/store/modelv1"
)

// writeReaction writes a single LineReaction or LineUnreaction event.
func writeReaction(ms *MessageStore, channelName string, msgTS string, sender string, senderID string, emoji string, remove bool) error {
	lineType := modelv1.LineReaction
	if remove {
		lineType = modelv1.LineUnreaction
	}
	line := modelv1.Line{
		Type: lineType,
		React: &modelv1.ReactLine{
			Ts:       ParseTimestamp(msgTS),
			MsgID:    msgTS,
			Sender:   sender,
			SenderID: senderID,
			Emoji:    emoji,
			Remove:   remove,
		},
	}
	return ms.AppendReaction(channelName, line)
}

// writeReactions writes LineReaction events for all reactions on a Slack message.
// Slack groups reactions by emoji with a user list; this expands them into
// one LineReaction per user per emoji. Deduplication is handled by compaction.
func writeReactions(ctx context.Context, ms *MessageStore, resolver *Resolver, channelName string, msg goslack.Message) error {
	var errs []error
	for _, reaction := range msg.Reactions {
		for _, userID := range reaction.Users {
			if err := writeReaction(ms, channelName, msg.Timestamp, resolver.UserName(ctx, userID), userID, reaction.Name, false); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}
