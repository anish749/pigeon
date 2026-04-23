package slack

import (
	"time"

	"github.com/anish749/pigeon/internal/store/modelv1"
	"github.com/anish749/pigeon/internal/store/modelv1/slackraw"
)

// buildMsgLine is the single construction site for a Slack MsgLine.
// All Write* methods build their payload here so callers that need the
// written line (e.g. the hub broadcast) can rely on a consistent shape.
func buildMsgLine(rs ResolvedSender, text string, ts time.Time, slackTS string, via modelv1.Via, raw slackraw.SlackRawContent) modelv1.MsgLine {
	return modelv1.MsgLine{
		ID:       slackTS,
		Ts:       ts,
		Sender:   rs.SenderName,
		SenderID: rs.SenderID,
		Via:      via,
		Text:     text,
		RawType:  modelv1.RawTypeSlack,
		Raw:      raw.AsSerializable(),
	}
}

// Write persists a message to the appropriate date file and returns the
// MsgLine that was written so the caller can publish it downstream (e.g.
// to the hub broadcast). Does not advance the cursor — only sync should
// do that via AdvanceCursor.
func (ms *MessageStore) Write(rs ResolvedSender, text string, ts time.Time, slackTS string, via modelv1.Via, raw slackraw.SlackRawContent) (modelv1.MsgLine, error) {
	msg := buildMsgLine(rs, text, ts, slackTS, via, raw)
	line := modelv1.Line{Type: modelv1.LineMessage, Msg: &msg}
	if err := ms.store.Append(ms.acct, rs.ChannelName, line); err != nil {
		return modelv1.MsgLine{}, err
	}
	return msg, nil
}

// WriteThreadMessage writes a message to a thread file and returns the
// MsgLine that was written. The isReply flag distinguishes actual thread
// replies from the context parent fetched when the thread is first seen.
func (ms *MessageStore) WriteThreadMessage(rs ResolvedSender, threadTS, text string, ts time.Time, slackTS string, isReply bool, via modelv1.Via, raw slackraw.SlackRawContent) (modelv1.MsgLine, error) {
	msg := buildMsgLine(rs, text, ts, slackTS, via, raw)
	msg.Reply = isReply
	line := modelv1.Line{Type: modelv1.LineMessage, Msg: &msg}
	if err := ms.store.AppendThread(ms.acct, rs.ChannelName, threadTS, line); err != nil {
		return modelv1.MsgLine{}, err
	}
	return msg, nil
}

// WriteThreadContext writes a channel context message to a thread file.
// Via is not set for context messages — they are historical fetches that
// predate the pigeon identity model.
func (ms *MessageStore) WriteThreadContext(rs ResolvedSender, threadTS, text string, ts time.Time, slackTS string, raw slackraw.SlackRawContent) error {
	msg := buildMsgLine(rs, text, ts, slackTS, "", raw)
	line := modelv1.Line{Type: modelv1.LineMessage, Msg: &msg}
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
			RawType:  modelv1.RawTypeSlack,
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
