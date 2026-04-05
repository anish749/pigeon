package modelv1

// ResolvedMsg is a message with its associated reactions attached.
type ResolvedMsg struct {
	MsgLine
	Reactions []ReactLine
}

// ResolvedDateFile is a compacted, resolved conversation ready for reading.
// Messages are sorted by timestamp with reactions grouped onto each message.
type ResolvedDateFile struct {
	Messages []ResolvedMsg
}

// ResolvedThreadFile is a compacted, resolved thread ready for reading.
type ResolvedThreadFile struct {
	Parent  ResolvedMsg
	Replies []ResolvedMsg
	Context []ResolvedMsg
}

// Resolve transforms a compacted DateFile into a ResolvedDateFile by
// grouping reactions onto their parent messages.
func Resolve(f *DateFile) *ResolvedDateFile {
	if f == nil {
		return &ResolvedDateFile{}
	}

	reactionsByMsg := groupReactions(f.Reactions)

	msgs := make([]ResolvedMsg, len(f.Messages))
	for i, m := range f.Messages {
		msgs[i] = ResolvedMsg{
			MsgLine:   m,
			Reactions: reactionsByMsg[m.ID],
		}
	}

	return &ResolvedDateFile{Messages: msgs}
}

// ResolveThread transforms a compacted ThreadFile into a ResolvedThreadFile.
func ResolveThread(f *ThreadFile) *ResolvedThreadFile {
	if f == nil {
		return nil
	}

	reactionsByMsg := groupReactions(f.Reactions)

	resolveSlice := func(msgs []MsgLine) []ResolvedMsg {
		out := make([]ResolvedMsg, len(msgs))
		for i, m := range msgs {
			out[i] = ResolvedMsg{
				MsgLine:   m,
				Reactions: reactionsByMsg[m.ID],
			}
		}
		return out
	}

	return &ResolvedThreadFile{
		Parent: ResolvedMsg{
			MsgLine:   f.Parent,
			Reactions: reactionsByMsg[f.Parent.ID],
		},
		Replies: resolveSlice(f.Replies),
		Context: resolveSlice(f.Context),
	}
}

func groupReactions(reactions []ReactLine) map[string][]ReactLine {
	m := make(map[string][]ReactLine)
	for _, r := range reactions {
		m[r.MsgID] = append(m[r.MsgID], r)
	}
	return m
}
