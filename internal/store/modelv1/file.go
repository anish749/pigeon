package modelv1

import (
	"sort"
	"strings"
	"time"
)

// ParseDateFile parses raw file bytes into a DateFile. Lines that fail to
// parse are skipped. The returned DateFile is raw (not compacted).
func ParseDateFile(data []byte) (*DateFile, error) {
	f := &DateFile{}
	for _, raw := range splitLines(data) {
		line, err := Parse(raw)
		if err != nil {
			continue // skip unparseable lines
		}
		classifyIntoDateFile(f, line)
	}
	return f, nil
}

// MarshalDateFile serialises a DateFile to bytes. All events are interleaved
// in chronological order by timestamp.
func MarshalDateFile(f *DateFile) []byte {
	lines := collectDateFileLines(f)
	sort.SliceStable(lines, func(i, j int) bool {
		return lineTs(lines[i]).Before(lineTs(lines[j]))
	})

	var b strings.Builder
	for _, l := range lines {
		b.WriteString(Marshal(l))
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

// ParseThreadFile parses raw file bytes into a ThreadFile. The thread file
// has structure: parent (first message), replies (indented), separator,
// context messages, and reactions/edits/deletes.
func ParseThreadFile(data []byte) (*ThreadFile, error) {
	f := &ThreadFile{}
	parentSet := false
	afterSeparator := false

	for _, raw := range splitLines(data) {
		line, err := Parse(raw)
		if err != nil {
			continue
		}

		switch line.Type {
		case LineSeparator:
			afterSeparator = true

		case LineMessage:
			if line.Msg.Reply {
				f.Replies = append(f.Replies, *line.Msg)
			} else if !parentSet {
				f.Parent = *line.Msg
				parentSet = true
			} else if afterSeparator {
				f.Context = append(f.Context, *line.Msg)
			} else {
				// Non-reply, non-parent, before separator: treat as context
				// (shouldn't happen in well-formed files, but handle gracefully)
				f.Context = append(f.Context, *line.Msg)
			}

		case LineReaction, LineUnreaction:
			f.Reactions = append(f.Reactions, *line.React)

		case LineEdit:
			f.Edits = append(f.Edits, *line.Edit)

		case LineDelete:
			f.Deletes = append(f.Deletes, *line.Delete)
		}
	}
	return f, nil
}

// MarshalThreadFile serialises a ThreadFile to bytes in the correct section
// order: parent, replies, separator, context, then reactions/edits/deletes.
func MarshalThreadFile(f *ThreadFile) []byte {
	var b strings.Builder

	// Parent
	b.WriteString(Marshal(Line{Type: LineMessage, Msg: &f.Parent}))
	b.WriteByte('\n')

	// Replies
	for i := range f.Replies {
		r := f.Replies[i]
		r.Reply = true // ensure indent
		b.WriteString(Marshal(Line{Type: LineMessage, Msg: &r}))
		b.WriteByte('\n')
	}

	// Separator + context (only if there's context)
	if len(f.Context) > 0 {
		b.WriteString(Marshal(Line{Type: LineSeparator}))
		b.WriteByte('\n')
		for i := range f.Context {
			b.WriteString(Marshal(Line{Type: LineMessage, Msg: &f.Context[i]}))
			b.WriteByte('\n')
		}
	}

	// Reactions, edits, deletes — sorted by timestamp
	var tail []Line
	for i := range f.Reactions {
		r := f.Reactions[i]
		if r.Remove {
			tail = append(tail, Line{Type: LineUnreaction, React: &r})
		} else {
			tail = append(tail, Line{Type: LineReaction, React: &r})
		}
	}
	for i := range f.Edits {
		tail = append(tail, Line{Type: LineEdit, Edit: &f.Edits[i]})
	}
	for i := range f.Deletes {
		tail = append(tail, Line{Type: LineDelete, Delete: &f.Deletes[i]})
	}
	sort.SliceStable(tail, func(i, j int) bool {
		return lineTs(tail[i]).Before(lineTs(tail[j]))
	})
	for _, l := range tail {
		b.WriteString(Marshal(l))
		b.WriteByte('\n')
	}

	return []byte(b.String())
}

// --- helpers ---

func splitLines(data []byte) []string {
	s := string(data)
	if s == "" {
		return nil
	}
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	var out []string
	for _, l := range lines {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

func classifyIntoDateFile(f *DateFile, line Line) {
	switch line.Type {
	case LineMessage:
		f.Messages = append(f.Messages, *line.Msg)
	case LineReaction, LineUnreaction:
		f.Reactions = append(f.Reactions, *line.React)
	case LineEdit:
		f.Edits = append(f.Edits, *line.Edit)
	case LineDelete:
		f.Deletes = append(f.Deletes, *line.Delete)
	}
}

func collectDateFileLines(f *DateFile) []Line {
	var lines []Line
	for i := range f.Messages {
		lines = append(lines, Line{Type: LineMessage, Msg: &f.Messages[i]})
	}
	for i := range f.Reactions {
		r := f.Reactions[i]
		if r.Remove {
			lines = append(lines, Line{Type: LineUnreaction, React: &r})
		} else {
			lines = append(lines, Line{Type: LineReaction, React: &r})
		}
	}
	for i := range f.Edits {
		lines = append(lines, Line{Type: LineEdit, Edit: &f.Edits[i]})
	}
	for i := range f.Deletes {
		lines = append(lines, Line{Type: LineDelete, Delete: &f.Deletes[i]})
	}
	return lines
}

func lineTs(l Line) time.Time {
	switch l.Type {
	case LineMessage:
		return l.Msg.Ts
	case LineReaction, LineUnreaction:
		return l.React.Ts
	case LineEdit:
		return l.Edit.Ts
	case LineDelete:
		return l.Delete.Ts
	default:
		return time.Time{}
	}
}
