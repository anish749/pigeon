package slack

import (
	"context"
	"errors"

	goslack "github.com/slack-go/slack"

)

// writeReactions writes LineReaction events for all reactions on a Slack message.
// Slack groups reactions by emoji with a user list; this expands them into
// one LineReaction per user per emoji. Deduplication is handled by compaction.
func writeReactions(ctx context.Context, ms *MessageStore, resolver *Resolver, channelName string, msg goslack.Message) error {
	var errs []error
	for _, reaction := range msg.Reactions {
		for _, userID := range reaction.Users {
			userName, err := resolver.UserName(ctx, userID)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if _, err := ms.AppendReaction(channelName, msg.Timestamp, userName, userID, reaction.Name, false); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}
