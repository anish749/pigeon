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
// MsgLine that was written. ThreadTS and ThreadID are both stamped on
// replies (with the same value, since Slack's parent identifier is a TS
// that also serves as its message ID) so the stored JSONL is
// self-describing and greppable from either vocabulary.
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
	if isReply {
		msg.ThreadTS = threadTS
		msg.ThreadID = threadTS
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

// AppendReaction stores a reaction or unreaction event and returns the
// ReactLine that was written so the caller can forward it downstream
// (e.g. to the hub).
//
// When threadTS is non-empty, the reaction targets a thread reply: the
// line is appended to the thread file so ParseThreadFile + CompactThread
// reconcile it onto the reply on read, and ThreadTS / ThreadID are
// stamped on the line. Otherwise the line lands in the date file with
// no thread tags — matching the routing for top-level messages and
// thread parents.
func (ms *MessageStore) AppendReaction(channelName, msgTS, threadTS, sender, senderID, emoji string, remove bool) (modelv1.ReactLine, error) {
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
	if threadTS != "" {
		react.ThreadTS = threadTS
		react.ThreadID = threadTS
	}
	line := modelv1.Line{Type: lineType, React: &react}
	var err error
	if threadTS != "" {
		err = ms.store.AppendThread(ms.acct, channelName, threadTS, line)
	} else {
		err = ms.store.Append(ms.acct, channelName, line)
	}
	if err != nil {
		return modelv1.ReactLine{}, err
	}
	return react, nil
}

// AppendEdit stores a message edit event for an in-conversation message.
// When threadTS is non-empty (i.e. the edited message lives in a thread),
// the edit line is appended to the thread file so ParseThreadFile +
// CompactThread can reconcile it onto the reply on read. Top-level edits
// keep the historical behaviour and land in the date file.
//
// The line itself stamps both ThreadTS and ThreadID with the same value —
// the file location and the line content carry redundant thread context
// so each surface (filename-based read, JSON-grep, notification format)
// can find the parent independently. See modelv1.MsgLine for the full
// grep-friendly rationale.
func (ms *MessageStore) AppendEdit(rs ResolvedSender, msgTS, threadTS, text string, ts time.Time, raw slackraw.SlackRawContent) (modelv1.EditLine, error) {
	edit := modelv1.EditLine{
		Ts:       ts,
		MsgID:    msgTS,
		Sender:   rs.SenderName,
		SenderID: rs.SenderID,
		Text:     text,
		RawType:  modelv1.RawTypeSlack,
		Raw:      raw.AsSerializable(),
	}
	if threadTS != "" {
		edit.ThreadTS = threadTS
		edit.ThreadID = threadTS
	}
	line := modelv1.Line{Type: modelv1.LineEdit, Edit: &edit}
	var err error
	if threadTS != "" {
		err = ms.store.AppendThread(ms.acct, rs.ChannelName, threadTS, line)
	} else {
		err = ms.store.Append(ms.acct, rs.ChannelName, line)
	}
	if err != nil {
		return modelv1.EditLine{}, err
	}
	return edit, nil
}

// AppendDelete stores a message delete event for an in-conversation
// message. Routing mirrors AppendEdit: thread targets land in the thread
// file (where CompactThread will drop the matching reply on read);
// top-level deletes go to the date file.
func (ms *MessageStore) AppendDelete(rs ResolvedSender, msgTS, threadTS string, ts time.Time) (modelv1.DeleteLine, error) {
	del := modelv1.DeleteLine{
		Ts:       ts,
		MsgID:    msgTS,
		Sender:   rs.SenderName,
		SenderID: rs.SenderID,
	}
	if threadTS != "" {
		del.ThreadTS = threadTS
		del.ThreadID = threadTS
	}
	line := modelv1.Line{Type: modelv1.LineDelete, Delete: &del}
	var err error
	if threadTS != "" {
		err = ms.store.AppendThread(ms.acct, rs.ChannelName, threadTS, line)
	} else {
		err = ms.store.Append(ms.acct, rs.ChannelName, line)
	}
	if err != nil {
		return modelv1.DeleteLine{}, err
	}
	return del, nil
}
