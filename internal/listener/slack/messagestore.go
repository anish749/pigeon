package slack

import (
	"time"

	"github.com/anish749/pigeon/internal/store/modelv1"
	"github.com/anish749/pigeon/internal/store/modelv1/slackraw"
)

// Write persists a message to the appropriate date file and returns the
// MsgLine that was written so the caller can publish it downstream (e.g.
// to the hub broadcast). Does not advance the cursor — only sync should
// do that via AdvanceCursor.
func (ms *MessageStore) Write(rs ResolvedSender, text string, ts time.Time, slackTS string, via modelv1.Via, raw slackraw.SlackRawContent) (modelv1.MsgLine, error) {
	msg := modelv1.MsgLine{
		ID:       slackTS,
		Ts:       ts,
		Sender:   rs.SenderName,
		SenderID: rs.SenderID,
		Via:      via,
		Text:     text,
		RawType:  modelv1.RawTypeSlack,
		Raw:      raw.AsSerializable(),
	}
	line := modelv1.Line{Type: modelv1.LineMessage, Msg: &msg}
	if err := ms.store.Append(ms.acct, rs.ChannelName, line); err != nil {
		return modelv1.MsgLine{}, err
	}
	return msg, nil
}

// WriteThreadMessage writes a message to a thread file and returns the
// MsgLine that was written.
func (ms *MessageStore) WriteThreadMessage(rs ResolvedSender, threadTS, text string, ts time.Time, slackTS string, isReply bool, via modelv1.Via, raw slackraw.SlackRawContent) (modelv1.MsgLine, error) {
	msg := modelv1.MsgLine{
		ID:       slackTS,
		Ts:       ts,
		Sender:   rs.SenderName,
		SenderID: rs.SenderID,
		Via:      via,
		Text:     text,
		Reply:    isReply,
		RawType:  modelv1.RawTypeSlack,
		Raw:      raw.AsSerializable(),
	}
	line := modelv1.Line{Type: modelv1.LineMessage, Msg: &msg}
	if err := ms.store.AppendThread(ms.acct, rs.ChannelName, threadTS, line); err != nil {
		return modelv1.MsgLine{}, err
	}
	return msg, nil
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
			RawType:  modelv1.RawTypeSlack,
			Raw:      raw.AsSerializable(),
		},
	}
	return ms.store.AppendThread(ms.acct, rs.ChannelName, threadTS, line)
}

// AppendReaction stores a reaction or unreaction event in the date file
// corresponding to the target message's timestamp, and returns the ReactLine
// that was written so the caller can forward it downstream (e.g. to the hub).
func (ms *MessageStore) AppendReaction(channelName, msgTS, sender, senderID, emoji string, remove bool) (modelv1.ReactLine, error) {
	lineType := modelv1.LineReaction
	if remove {
		lineType = modelv1.LineUnreaction
	}
	react := modelv1.ReactLine{
		Ts:       ParseTimestamp(msgTS),
		MsgID:    msgTS,
		Sender:   sender,
		SenderID: senderID,
		Emoji:    emoji,
		Remove:   remove,
	}
	line := modelv1.Line{Type: lineType, React: &react}
	if err := ms.store.Append(ms.acct, channelName, line); err != nil {
		return modelv1.ReactLine{}, err
	}
	return react, nil
}

// AppendEdit stores a message edit event in the date file corresponding
// to the target message's timestamp. threadTS is the thread root TS when
// the edited message is a thread reply, or empty for top-level messages.
// Returns the EditLine that was written so callers can forward it.
//
// (See issues/bugs.md: when threadTS is non-empty, the line still goes to
// the date file rather than the thread file — that's a separate compaction
// bug. Persisting threadTS here is the prerequisite for that fix.)
func (ms *MessageStore) AppendEdit(rs ResolvedSender, msgTS, threadTS, text string, ts time.Time, raw slackraw.SlackRawContent) (modelv1.EditLine, error) {
	edit := modelv1.EditLine{
		Ts:       ts,
		MsgID:    msgTS,
		ThreadTS: threadTS,
		Sender:   rs.SenderName,
		SenderID: rs.SenderID,
		Text:     text,
		RawType:  modelv1.RawTypeSlack,
		Raw:      raw.AsSerializable(),
	}
	line := modelv1.Line{Type: modelv1.LineEdit, Edit: &edit}
	if err := ms.store.Append(ms.acct, rs.ChannelName, line); err != nil {
		return modelv1.EditLine{}, err
	}
	return edit, nil
}

// AppendDelete stores a message delete event in the date file corresponding
// to the target message's timestamp. threadTS is set when the deleted
// message lived in a thread.
func (ms *MessageStore) AppendDelete(rs ResolvedSender, msgTS, threadTS string, ts time.Time) (modelv1.DeleteLine, error) {
	del := modelv1.DeleteLine{
		Ts:       ts,
		MsgID:    msgTS,
		ThreadTS: threadTS,
		Sender:   rs.SenderName,
		SenderID: rs.SenderID,
	}
	line := modelv1.Line{Type: modelv1.LineDelete, Delete: &del}
	if err := ms.store.Append(ms.acct, rs.ChannelName, line); err != nil {
		return modelv1.DeleteLine{}, err
	}
	return del, nil
}
