package modelv1

import "sort"

// reactKey identifies a unique reaction tuple for dedup and reconciliation.
type reactKey struct {
	MsgID    string
	Emoji    string
	SenderID string
}

// Compact takes a raw DateFile and returns a compacted one. It deduplicates
// messages and reactions, reconciles react/unreact pairs, applies edits and
// deletes, and sorts messages by timestamp. The input is not mutated.
func Compact(f *DateFile) *DateFile {
	if f == nil {
		return &DateFile{}
	}

	msgs := dedupMessages(f.Messages)
	reactions := dedupReactions(f.Reactions)
	reactions = reconcileReactions(reactions)
	msgs = applyEdits(msgs, f.Edits)
	msgs, reactions = applyDeletes(msgs, reactions, f.Edits, f.Deletes)

	sort.SliceStable(msgs, func(i, j int) bool {
		return msgs[i].Ts.Before(msgs[j].Ts)
	})

	return &DateFile{
		Messages:  msgs,
		Reactions: reactions,
	}
}

// CompactThread applies the same compaction logic as Compact but for thread
// files. Edits and deletes can target the parent, any reply, or any context
// message. If the parent is deleted, nil is returned.
func CompactThread(f *ThreadFile) *ThreadFile {
	if f == nil {
		return nil
	}

	// Combine all messages into a single slice for uniform processing.
	// Tag each message so we can separate them back out.
	type tagged struct {
		msg    MsgLine
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

	// Deduplicate messages by ID, keeping first occurrence.
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

	// Apply edits to all messages.
	latestEdits := latestEditByMsg(f.Edits)
	for i, t := range deduped {
		if e, ok := latestEdits[t.msg.ID]; ok {
			deduped[i].msg.Text = e.Text
			deduped[i].msg.Attachments = cloneAttachments(e.Attachments)
		}
	}

	// Apply deletes: remove messages and associated reactions.
	deleted := make(map[string]bool)
	for _, d := range f.Deletes {
		deleted[d.MsgID] = true
	}

	// If parent is deleted, return nil.
	if len(deduped) > 0 && deduped[0].source == "parent" && deleted[deduped[0].msg.ID] {
		return nil
	}

	var surviving []tagged
	for _, t := range deduped {
		if !deleted[t.msg.ID] {
			surviving = append(surviving, t)
		}
	}

	// Remove reactions referencing deleted messages.
	var survivingReactions []ReactLine
	for _, r := range reactions {
		if !deleted[r.MsgID] {
			survivingReactions = append(survivingReactions, r)
		}
	}

	// Separate back into parent, replies, context.
	var parent MsgLine
	var replies, context []MsgLine
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

	// Sort replies and context by timestamp.
	sort.SliceStable(replies, func(i, j int) bool {
		return replies[i].Ts.Before(replies[j].Ts)
	})
	sort.SliceStable(context, func(i, j int) bool {
		return context[i].Ts.Before(context[j].Ts)
	})

	return &ThreadFile{
		Parent:    parent,
		Replies:   replies,
		Context:   context,
		Reactions: survivingReactions,
	}
}

// AggregateReactions groups reactions by message ID, reconciles react/unreact
// pairs per (message ID, emoji, sender ID), and returns the final reaction
// state per message. Only surviving reacts (Remove=false) are included.
func AggregateReactions(reactions []ReactLine) map[string][]ReactLine {
	reconciled := reconcileReactions(dedupReactions(reactions))

	result := make(map[string][]ReactLine)
	for _, r := range reconciled {
		result[r.MsgID] = append(result[r.MsgID], r)
	}
	return result
}

// dedupMessages removes duplicate messages by ID, keeping the first occurrence.
func dedupMessages(msgs []MsgLine) []MsgLine {
	seen := make(map[string]bool)
	var out []MsgLine
	for _, m := range msgs {
		if seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		out = append(out, m)
	}
	return out
}

// dedupReactions removes exact duplicate reaction lines. Two reaction lines
// are duplicates if they share the same (MsgID, Emoji, SenderID, Remove, Ts)
// tuple — i.e. the same event was appended twice. Different timestamps or
// different Remove values represent distinct events and are preserved for
// reconciliation.
func dedupReactions(reactions []ReactLine) []ReactLine {
	type dedupKey struct {
		MsgID    string
		Emoji    string
		SenderID string
		Remove   bool
		Ts       int64 // UnixNano for comparison
	}
	seen := make(map[dedupKey]bool)
	var out []ReactLine
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

// reconcileReactions replays react/unreact events per (MsgID, Emoji, SenderID)
// tuple in timestamp order and returns only the surviving reacts.
func reconcileReactions(reactions []ReactLine) []ReactLine {
	// Group by key.
	groups := make(map[reactKey][]ReactLine)
	for _, r := range reactions {
		k := reactKey{r.MsgID, r.Emoji, r.SenderID}
		groups[k] = append(groups[k], r)
	}

	var out []ReactLine
	for _, events := range groups {
		// Sort by timestamp to replay in order.
		sort.SliceStable(events, func(i, j int) bool {
			return events[i].Ts.Before(events[j].Ts)
		})

		// Replay: track the final state.
		var last *ReactLine
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

	// Sort output by timestamp for deterministic ordering.
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Ts.Before(out[j].Ts)
	})

	return out
}

// latestEditByMsg returns the most recent edit for each message ID.
func latestEditByMsg(edits []EditLine) map[string]EditLine {
	latest := make(map[string]EditLine)
	for _, e := range edits {
		if prev, ok := latest[e.MsgID]; !ok || e.Ts.After(prev.Ts) {
			latest[e.MsgID] = e
		}
	}
	return latest
}

// applyEdits replaces message text and attachments with the most recent edit.
// Edit lines are consumed and not included in the output.
func applyEdits(msgs []MsgLine, edits []EditLine) []MsgLine {
	latest := latestEditByMsg(edits)
	if len(latest) == 0 {
		return msgs
	}

	out := make([]MsgLine, len(msgs))
	copy(out, msgs)
	for i, m := range out {
		if e, ok := latest[m.ID]; ok {
			out[i].Text = e.Text
			out[i].Attachments = cloneAttachments(e.Attachments)
		}
	}
	return out
}

// applyDeletes removes deleted messages and any reactions, edits, or unreactions
// that reference the deleted message. Returns the surviving messages and reactions.
func applyDeletes(msgs []MsgLine, reactions []ReactLine, edits []EditLine, deletes []DeleteLine) ([]MsgLine, []ReactLine) {
	deleted := make(map[string]bool)
	for _, d := range deletes {
		deleted[d.MsgID] = true
	}
	if len(deleted) == 0 {
		return msgs, reactions
	}

	var outMsgs []MsgLine
	for _, m := range msgs {
		if !deleted[m.ID] {
			outMsgs = append(outMsgs, m)
		}
	}

	var outReactions []ReactLine
	for _, r := range reactions {
		if !deleted[r.MsgID] {
			outReactions = append(outReactions, r)
		}
	}

	return outMsgs, outReactions
}

// cloneAttachments returns a copy of the attachment slice.
func cloneAttachments(a []Attachment) []Attachment {
	if a == nil {
		return nil
	}
	out := make([]Attachment, len(a))
	copy(out, a)
	return out
}
