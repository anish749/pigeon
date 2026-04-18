package slack

import (
	"time"

	"github.com/anish749/pigeon/internal/store/modelv1"
	"github.com/anish749/pigeon/internal/store/modelv1/slackraw"
)

// Write persists a message to the appropriate date file. Does not advance the
// cursor — only sync should do that via AdvanceCursor.
func (ms *MessageStore) Write(rs ResolvedSender, text string, ts time.Time, slackTS string, via modelv1.Via, raw slackraw.SlackRawContent) error {
	line := modelv1.Line{
		Type: modelv1.LineMessage,
		Msg: &modelv1.MsgLine{
			ID:       slackTS,
			Ts:       ts,
			Sender:   rs.SenderName,
			SenderID: rs.SenderID,
			Via:      via,
			Text:     text,
			Raw:      raw.AsSerializable(),
		},
	}
	return ms.store.Append(ms.acct, rs.ChannelName, line)
}

// WriteThreadMessage writes a message to a thread file.
func (ms *MessageStore) WriteThreadMessage(rs ResolvedSender, threadTS, text string, ts time.Time, slackTS string, isReply bool, via modelv1.Via, raw slackraw.SlackRawContent) error {
	line := modelv1.Line{
		Type: modelv1.LineMessage,
		Msg: &modelv1.MsgLine{
			ID:       slackTS,
			Ts:       ts,
			Sender:   rs.SenderName,
			SenderID: rs.SenderID,
			Via:      via,
			Text:     text,
			Reply:    isReply,
			Raw:      raw.AsSerializable(),
		},
	}
	return ms.store.AppendThread(ms.acct, rs.ChannelName, threadTS, line)
}

// WriteThreadContext writes a channel context message to a thread file.
func (ms *MessageStore) WriteThreadContext(rs ResolvedSender, threadTS, text string, ts time.Time, slackTS string, raw slackraw.SlackRawContent) error {
	line := modelv1.Line{
		Type: modelv1.LineMessage,
		Msg: &modelv1.MsgLine{
			ID:       slackTS,
			Ts:       ts,
			Sender:   rs.SenderName,
			SenderID: rs.SenderID,
			Text:     text,
			Raw:      raw.AsSerializable(),
		},
	}
	return ms.store.AppendThread(ms.acct, rs.ChannelName, threadTS, line)
}

// AppendReaction stores a reaction or unreaction event in the date file
// corresponding to the target message's timestamp.
func (ms *MessageStore) AppendReaction(channelName, msgTS, sender, senderID, emoji string, remove bool) error {
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
	return ms.store.Append(ms.acct, channelName, line)
}

// AppendEdit stores a message edit event in the date file corresponding
// to the target message's timestamp.
func (ms *MessageStore) AppendEdit(rs ResolvedSender, msgTS, text string, ts time.Time, raw slackraw.SlackRawContent) error {
	line := modelv1.Line{
		Type: modelv1.LineEdit,
		Edit: &modelv1.EditLine{
			Ts:       ts,
			MsgID:    msgTS,
			Sender:   rs.SenderName,
			SenderID: rs.SenderID,
			Text:     text,
			Raw:      raw.AsSerializable(),
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
