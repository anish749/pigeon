package hub

import (
	"time"

	"github.com/anish/claude-msg-utils/internal/store"
)

// StoreMessageReader implements MessageReader using the store package.
type StoreMessageReader struct{}

func (StoreMessageReader) ReadSince(platform, account, conversation string, since time.Time) ([]ParsedMessage, error) {
	lines, err := store.ReadMessagesSince(platform, account, conversation, since)
	if err != nil {
		return nil, err
	}

	var messages []ParsedMessage
	for _, line := range lines {
		parsed := store.ParseMessageLine(line)
		if parsed == nil {
			continue
		}
		messages = append(messages, ParsedMessage{
			Timestamp: parsed.Timestamp,
			Sender:    parsed.Sender,
			Text:      parsed.Text,
		})
	}
	return messages, nil
}
