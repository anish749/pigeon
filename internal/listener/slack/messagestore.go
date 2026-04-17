package slack

import (
	"time"

	"github.com/anish749/pigeon/internal/store/modelv1"
)

// Write persists a message to the appropriate date file. Does not advance the
// cursor — only sync should do that via AdvanceCursor.
func (ms *MessageStore) Write(channelID, channelName, sender, senderID, text string, ts time.Time, slackTS string, via modelv1.Via) error {
	line := modelv1.Line{
		Type: modelv1.LineMessage,
		Msg: &modelv1.MsgLine{
			ID:       slackTS,
			Ts:       ts,
			Sender:   sender,
			SenderID: senderID,
			Via:      via,
			Text:     text,
		},
	}
	return ms.store.Append(ms.acct, channelName, line)
}

// WriteThreadMessage writes a message to a thread file.
func (ms *MessageStore) WriteThreadMessage(channelName, threadTS, sender, senderID, text string, ts time.Time, slackTS string, isReply bool, via modelv1.Via) error {
	line := modelv1.Line{
		Type: modelv1.LineMessage,
		Msg: &modelv1.MsgLine{
			ID:       slackTS,
			Ts:       ts,
			Sender:   sender,
			SenderID: senderID,
			Via:      via,
			Text:     text,
			Reply:    isReply,
		},
	}
	return ms.store.AppendThread(ms.acct, channelName, threadTS, line)
}

// WriteThreadContext writes a channel context message to a thread file.
func (ms *MessageStore) WriteThreadContext(channelName, threadTS, sender, senderID, text string, ts time.Time, slackTS string) error {
	line := modelv1.Line{
		Type: modelv1.LineMessage,
		Msg: &modelv1.MsgLine{
			ID:       slackTS,
			Ts:       ts,
			Sender:   sender,
			SenderID: senderID,
			Text:     text,
		},
	}
	return ms.store.AppendThread(ms.acct, channelName, threadTS, line)
}

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
