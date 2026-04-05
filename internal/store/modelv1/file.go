package modelv1

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ParseDateFile parses raw file bytes into a DateFile. Unparseable lines are
// skipped. If any lines fail to parse, the returned error collects them but
// the successfully parsed lines are still returned.
func ParseDateFile(data []byte) (*DateFile, error) {
	f := &DateFile{}
	var errs []error
	for _, raw := range splitLines(data) {
		line, err := Parse(raw)
		if err != nil {
			errs = append(errs, fmt.Errorf("skip line: %w", err))
			continue
		}
		classifyIntoDateFile(f, line)
	}
	return f, errors.Join(errs...)
}

// MarshalDateFile serialises a DateFile to bytes. All events are interleaved
// in chronological order by timestamp.
func MarshalDateFile(f *DateFile) ([]byte, error) {
	lines := collectDateFileLines(f)
	sort.SliceStable(lines, func(i, j int) bool {
		return lines[i].Ts().Before(lines[j].Ts())
	})

	var b []byte
	for _, l := range lines {
		data, err := Marshal(l)
		if err != nil {
			return nil, fmt.Errorf("marshal date file: %w", err)
		}
		b = append(b, data...)
		b = append(b, '\n')
	}
	return b, nil
}

// ParseThreadFile parses raw file bytes into a ThreadFile. The thread file
// has structure: parent (first message), replies (reply=true), separator,
// context messages, and reactions/edits/deletes.
func ParseThreadFile(data []byte) (*ThreadFile, error) {
	f := &ThreadFile{}
	parentSet := false
	afterSeparator := false

	var errs []error
	for _, raw := range splitLines(data) {
		line, err := Parse(raw)
		if err != nil {
			errs = append(errs, fmt.Errorf("skip line: %w", err))
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
	return f, errors.Join(errs...)
}

// MarshalThreadFile serialises a ThreadFile to bytes in the correct section
// order: parent, replies, separator, context, then reactions/edits/deletes.
func MarshalThreadFile(f *ThreadFile) ([]byte, error) {
	var b []byte

	write := func(l Line) error {
		data, err := Marshal(l)
		if err != nil {
			return err
		}
		b = append(b, data...)
		b = append(b, '\n')
		return nil
	}

	// Parent
	if err := write(Line{Type: LineMessage, Msg: &f.Parent}); err != nil {
		return nil, fmt.Errorf("marshal thread file: %w", err)
	}

	// Replies
	for i := range f.Replies {
		r := f.Replies[i]
		r.Reply = true // ensure reply flag
		if err := write(Line{Type: LineMessage, Msg: &r}); err != nil {
			return nil, fmt.Errorf("marshal thread file: %w", err)
		}
	}

	// Separator + context (only if there's context)
	if len(f.Context) > 0 {
		if err := write(Line{Type: LineSeparator}); err != nil {
			return nil, fmt.Errorf("marshal thread file: %w", err)
		}
		for i := range f.Context {
			if err := write(Line{Type: LineMessage, Msg: &f.Context[i]}); err != nil {
				return nil, fmt.Errorf("marshal thread file: %w", err)
			}
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
		return tail[i].Ts().Before(tail[j].Ts())
	})
	for _, l := range tail {
		if err := write(l); err != nil {
			return nil, fmt.Errorf("marshal thread file: %w", err)
		}
	}

	return b, nil
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
