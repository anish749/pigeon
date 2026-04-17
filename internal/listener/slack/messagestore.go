package slack

import (
	"time"

	"github.com/anish749/pigeon/internal/store/modelv1"
)

// ResolvedSender holds the resolver-derived fields common to all incoming
// Slack events (messages, reactions, edits, deletes).
type ResolvedSender struct {
	ChannelName string
	SenderName  string
	SenderID    string
}

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
func (ms *MessageStore) AppendReaction(rs ResolvedSender, msgTS, emoji string, remove bool) error {
	lineType := modelv1.LineReaction
	if remove {
		lineType = modelv1.LineUnreaction
	}
	line := modelv1.Line{
		Type: lineType,
		React: &modelv1.ReactLine{
			Ts:       ParseTimestamp(msgTS),
			MsgID:    msgTS,
			Sender:   rs.SenderName,
			SenderID: rs.SenderID,
			Emoji:    emoji,
			Remove:   remove,
		},
	}
	return ms.store.Append(ms.acct, rs.ChannelName, line)
}

// AppendEdit stores a message edit event in the date file corresponding
// to the target message's timestamp.
func (ms *MessageStore) AppendEdit(rs ResolvedSender, msgTS, text string, ts time.Time) error {
	line := modelv1.Line{
		Type: modelv1.LineEdit,
		Edit: &modelv1.EditLine{
			Ts:       ts,
			MsgID:    msgTS,
			Sender:   rs.SenderName,
			SenderID: rs.SenderID,
			Text:     text,
		},
	}
	return ms.store.Append(ms.acct, rs.ChannelName, line)
}

// AppendDelete stores a message delete event in the date file corresponding
// to the target message's timestamp.
func (ms *MessageStore) AppendDelete(rs ResolvedSender, msgTS string, ts time.Time) error {
	line := modelv1.Line{
		Type: modelv1.LineDelete,
		Delete: &modelv1.DeleteLine{
			Ts:       ts,
			MsgID:    msgTS,
			Sender:   rs.SenderName,
			SenderID: rs.SenderID,
		},
	}
	return ms.store.Append(ms.acct, rs.ChannelName, line)
}
