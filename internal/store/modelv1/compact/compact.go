package compact

import (
	"sort"

	"github.com/anish749/pigeon/internal/store/modelv1"
)

// reactKey identifies a unique reaction tuple for dedup and reconciliation.
type reactKey struct {
	MsgID    string
	Emoji    string
	SenderID string
}

// Compact takes a raw DateFile and returns a cleaned, display-ready version.
// The input is not mutated.
//
// Steps applied in order:
//  1. Dedup messages by ID (keeps first occurrence).
//  2. Dedup reactions by (msgID, emoji, senderID, remove, ts).
//  3. Reconcile react/unreact pairs: for each (msgID, emoji, senderID) tuple,
//     replay events chronologically — an unreact cancels the preceding react.
//     Only surviving reacts are kept.
//  4. Apply edits: each message is updated to the text/attachments of its
//     latest EditLine (by timestamp).
//  5. Apply deletes: messages targeted by a DeleteLine are removed, along with
//     their reactions.
//  6. Sort surviving messages by timestamp (stable).
func Compact(f *modelv1.DateFile) *modelv1.DateFile {
	if f == nil {
		return &modelv1.DateFile{}
	}

	msgs := dedupMessages(f.Messages)
	reactions := dedupReactions(f.Reactions)
	reactions = reconcileReactions(reactions)
	msgs = applyEdits(msgs, f.Edits)
	msgs, reactions = applyDeletes(msgs, reactions, f.Edits, f.Deletes)

	sort.SliceStable(msgs, func(i, j int) bool {
		return msgs[i].Ts.Before(msgs[j].Ts)
	})

	return &modelv1.DateFile{
		Messages:  msgs,
		Reactions: reactions,
	}
}

// CompactThread applies the same compaction logic as Compact but for thread
// files. Edits and deletes can target the parent, any reply, or any context
// message. If the parent is deleted, nil is returned.
func CompactThread(f *modelv1.ThreadFile) *modelv1.ThreadFile {
	if f == nil {
		return nil
	}

	type tagged struct {
		msg    modelv1.MsgLine
		source string // "parent", "reply", "context"
	}

	var all []tagged
	all = append(all, tagged{msg: f.Parent, source: "parent"})
	for _, r := range f.Replies {
		all = append(all, tagged{msg: r, source: "reply"})
	}
	for _, c := range f.Context {
		all = append(all, tagged{msg: c, source: "context"})
	}

	seen := make(map[string]bool)
	var deduped []tagged
	for _, t := range all {
		if seen[t.msg.ID] {
			continue
		}
		seen[t.msg.ID] = true
		deduped = append(deduped, t)
	}

	reactions := dedupReactions(f.Reactions)
	reactions = reconcileReactions(reactions)

	latestEdits := latestEditByMsg(f.Edits)
	for i, t := range deduped {
		if e, ok := latestEdits[t.msg.ID]; ok {
			deduped[i].msg.Text = e.Text
			deduped[i].msg.Attachments = cloneAttachments(e.Attachments)
		}
	}

	deleted := make(map[string]bool)
	for _, d := range f.Deletes {
		deleted[d.MsgID] = true
	}

	if len(deduped) > 0 && deduped[0].source == "parent" && deleted[deduped[0].msg.ID] {
		return nil
	}

	var surviving []tagged
	for _, t := range deduped {
		if !deleted[t.msg.ID] {
			surviving = append(surviving, t)
		}
	}

	var survivingReactions []modelv1.ReactLine
	for _, r := range reactions {
		if !deleted[r.MsgID] {
			survivingReactions = append(survivingReactions, r)
		}
	}

	var parent modelv1.MsgLine
	var replies, context []modelv1.MsgLine
	for _, t := range surviving {
		switch t.source {
		case "parent":
			parent = t.msg
		case "reply":
			replies = append(replies, t.msg)
		case "context":
			context = append(context, t.msg)
		}
	}

	sort.SliceStable(replies, func(i, j int) bool {
		return replies[i].Ts.Before(replies[j].Ts)
	})
	sort.SliceStable(context, func(i, j int) bool {
		return context[i].Ts.Before(context[j].Ts)
	})

	return &modelv1.ThreadFile{
		Parent:    parent,
		Replies:   replies,
		Context:   context,
		Reactions: survivingReactions,
	}
}

// AggregateReactions groups reactions by message ID, reconciles react/unreact
// pairs per (message ID, emoji, sender ID), and returns the final reaction
// state per message. Only surviving reacts (Remove=false) are included.
func AggregateReactions(reactions []modelv1.ReactLine) map[string][]modelv1.ReactLine {
	reconciled := reconcileReactions(dedupReactions(reactions))

	result := make(map[string][]modelv1.ReactLine)
	for _, r := range reconciled {
		result[r.MsgID] = append(result[r.MsgID], r)
	}
	return result
}

// --- internal helpers ---

func dedupMessages(msgs []modelv1.MsgLine) []modelv1.MsgLine {
	seen := make(map[string]bool)
	var out []modelv1.MsgLine
	for _, m := range msgs {
		if seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		out = append(out, m)
	}
	return out
}

func dedupReactions(reactions []modelv1.ReactLine) []modelv1.ReactLine {
	type dedupKey struct {
		MsgID    string
		Emoji    string
		SenderID string
		Remove   bool
		Ts       int64
	}
	seen := make(map[dedupKey]bool)
	var out []modelv1.ReactLine
	for _, r := range reactions {
		k := dedupKey{r.MsgID, r.Emoji, r.SenderID, r.Remove, r.Ts.UnixNano()}
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, r)
	}
	return out
}

func reconcileReactions(reactions []modelv1.ReactLine) []modelv1.ReactLine {
	groups := make(map[reactKey][]modelv1.ReactLine)
	for _, r := range reactions {
		k := reactKey{r.MsgID, r.Emoji, r.SenderID}
		groups[k] = append(groups[k], r)
	}

	var out []modelv1.ReactLine
	for _, events := range groups {
		sort.SliceStable(events, func(i, j int) bool {
			return events[i].Ts.Before(events[j].Ts)
		})

		var last *modelv1.ReactLine
		for i := range events {
			if events[i].Remove {
				last = nil
			} else {
				last = &events[i]
			}
		}
		if last != nil {
			out = append(out, *last)
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Ts.Before(out[j].Ts)
	})

	return out
}

func latestEditByMsg(edits []modelv1.EditLine) map[string]modelv1.EditLine {
	latest := make(map[string]modelv1.EditLine)
	for _, e := range edits {
		if prev, ok := latest[e.MsgID]; !ok || e.Ts.After(prev.Ts) {
			latest[e.MsgID] = e
		}
	}
	return latest
}

func applyEdits(msgs []modelv1.MsgLine, edits []modelv1.EditLine) []modelv1.MsgLine {
	latest := latestEditByMsg(edits)
	if len(latest) == 0 {
		return msgs
	}

	out := make([]modelv1.MsgLine, len(msgs))
	copy(out, msgs)
	for i, m := range out {
		if e, ok := latest[m.ID]; ok {
			out[i].Text = e.Text
			out[i].Attachments = cloneAttachments(e.Attachments)
		}
	}
	return out
}

func applyDeletes(msgs []modelv1.MsgLine, reactions []modelv1.ReactLine, edits []modelv1.EditLine, deletes []modelv1.DeleteLine) ([]modelv1.MsgLine, []modelv1.ReactLine) {
	deleted := make(map[string]bool)
	for _, d := range deletes {
		deleted[d.MsgID] = true
	}
	if len(deleted) == 0 {
		return msgs, reactions
	}

	var outMsgs []modelv1.MsgLine
	for _, m := range msgs {
		if !deleted[m.ID] {
			outMsgs = append(outMsgs, m)
		}
	}

	var outReactions []modelv1.ReactLine
	for _, r := range reactions {
		if !deleted[r.MsgID] {
			outReactions = append(outReactions, r)
		}
	}

	return outMsgs, outReactions
}

func cloneAttachments(a []modelv1.Attachment) []modelv1.Attachment {
	if a == nil {
		return nil
	}
	out := make([]modelv1.Attachment, len(a))
	copy(out, a)
	return out
}

// Dedup removes duplicate Lines by ID (keep last occurrence) and applies
// delete semantics: an email-delete line removes the matching email.
// Lines without IDs (e.g. messaging lines, separators) are kept as-is.
func Dedup(lines []modelv1.Line) []modelv1.Line {
	lastIndex := make(map[string]int)
	deletedIDs := make(map[string]bool)

	for i, l := range lines {
		id, ok := l.ID()
		if ok {
			lastIndex[id] = i
		}
		if l.Type == modelv1.LineEmailDelete {
			deletedIDs[l.EmailDelete.ID] = true
		}
	}

	var result []modelv1.Line
	for i, l := range lines {
		id, ok := l.ID()

		if !ok {
			result = append(result, l)
			continue
		}
		if lastIndex[id] != i {
			continue
		}
		if l.Type == modelv1.LineEmailDelete {
			continue
		}
		if l.Type == modelv1.LineEmail && deletedIDs[id] {
			continue
		}

		result = append(result, l)
	}
	return result
}
