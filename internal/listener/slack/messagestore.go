package slack

import (
	"github.com/anish749/pigeon/internal/store/modelv1"
)

// AppendReaction stores a reaction or unreaction event in the date file
// corresponding to the target message's timestamp.
func (ms *MessageStore) AppendReaction(channelName string, line modelv1.Line) error {
	return ms.store.Append(ms.acct, channelName, line)
}

// AppendEdit stores a message edit event in the date file corresponding
// to the target message's timestamp.
func (ms *MessageStore) AppendEdit(channelName string, line modelv1.Line) error {
	return ms.store.Append(ms.acct, channelName, line)
}

// AppendDelete stores a message delete event in the date file corresponding
// to the target message's timestamp.
func (ms *MessageStore) AppendDelete(channelName string, line modelv1.Line) error {
	return ms.store.Append(ms.acct, channelName, line)
}
