package store

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store/modelv1"
	"github.com/anish749/pigeon/internal/store/modelv1/compact"
)

// ReadConversation pipeline:
//
//  1. Discover files. Date files are filtered by filename window (a directory-
//     layout contract). Thread files are listed whole — pre-filtering by
//     content would couple to MsgLine.Ts's JSON shape.
//  2. Parse + resolve into standalone date-file messages and thread groups.
//  3. Time-filter individually: each message and each thread member is checked
//     against the window using typed time.Time comparisons.
//  4. Thread completion: a thread with ≥ 1 surviving member is kept whole,
//     dragging its other members back in regardless of the window. Symmetric
//     — parent-in-window pulls replies, reply-in-window pulls parent.
//  5. Dedupe by ID: a parent that's in both the date file and the thread
//     file is dropped from the standalone list (the thread atom owns it).
//  6. Sort by thread sort-ts (parent's ts, or earliest reply ts for orphans);
//     standalones sort by own ts.
//  7. Interleave: each thread atom emits parent first, replies in ts order.
//  8. --last N positional tail trim. No filter at all returns the full
//     conversation; defaults belong to callers, not this layer.
func (s *FSStore) ReadConversation(acct account.Account, conversation string, opts ReadOpts) (*modelv1.ResolvedDateFile, error) {
	conv := s.convDir(acct, conversation)

	window, err := windowFromOpts(opts)
	if err != nil {
		return nil, err
	}

	dateFiles, threadFiles, err := s.discoverFiles(conv, window)
	if err != nil {
		return nil, err
	}

	var errs []error
	standalones, err := s.readDateFiles(dateFiles)
	if err != nil {
		errs = append(errs, err)
	}
	threadGroups, err := s.readThreadFiles(threadFiles)
	if err != nil {
		errs = append(errs, err)
	}

	if window.bounded() {
		standalones = filterMsgsByWindow(standalones, window)
		threadGroups = filterThreadsByWindow(threadGroups, window)
	}

	standalones = dedupParentsInThreads(standalones, threadGroups)

	result := interleaveAtoms(standalones, threadGroups)

	if opts.Last > 0 && len(result) > opts.Last {
		result = result[len(result)-opts.Last:]
	}

	return &modelv1.ResolvedDateFile{Messages: result}, errors.Join(errs...)
}

// readDateFiles parses every path as a date file, merges them, then runs
// compaction + reaction resolution as one batch so cross-file edits and
// reactions resolve correctly.
func (s *FSStore) readDateFiles(files []paths.MessagingDateFile) ([]modelv1.ResolvedMsg, error) {
	merged := &modelv1.DateFile{}
	var errs []error
	for _, f := range files {
		data, err := os.ReadFile(f.Path())
		if err != nil {
			errs = append(errs, fmt.Errorf("read %s: %w", f.Path(), err))
			continue
		}
		df, parseErr := modelv1.ParseDateFile(data)
		if parseErr != nil {
			slog.Warn("parse date file: some lines skipped", "file", f.Path(), "error", parseErr)
		}
		merged.Messages = append(merged.Messages, df.Messages...)
		merged.Reactions = append(merged.Reactions, df.Reactions...)
		merged.Edits = append(merged.Edits, df.Edits...)
		merged.Deletes = append(merged.Deletes, df.Deletes...)
	}
	compacted := compact.Compact(merged)
	resolved := modelv1.Resolve(compacted)
	return resolved.Messages, errors.Join(errs...)
}

// readThreadFiles parses each path as a thread file. Each file becomes one
// ResolvedThreadFile; orphan files (parent line never written) come back with
// Parent.ID == "" — sort and interleave fall back to the earliest reply ts.
func (s *FSStore) readThreadFiles(files []paths.ThreadFile) ([]*modelv1.ResolvedThreadFile, error) {
	var groups []*modelv1.ResolvedThreadFile
	var errs []error
	for _, f := range files {
		data, err := os.ReadFile(f.Path())
		if err != nil {
			errs = append(errs, fmt.Errorf("read %s: %w", f.Path(), err))
			continue
		}
		tf, parseErr := modelv1.ParseThreadFile(data)
		if parseErr != nil {
			slog.Warn("parse thread file: some lines skipped", "file", f.Path(), "error", parseErr)
		}
		resolved := modelv1.ResolveThread(compact.CompactThread(tf))
		if resolved == nil {
			continue
		}
		if resolved.Parent.ID == "" && len(resolved.Replies) > 0 {
			slog.Warn("thread file missing parent line, surfacing replies as orphans",
				"file", f.Path(), "replies", len(resolved.Replies))
		}
		groups = append(groups, resolved)
	}
	return groups, errors.Join(errs...)
}

func filterMsgsByWindow(msgs []modelv1.ResolvedMsg, w readWindow) []modelv1.ResolvedMsg {
	out := msgs[:0:0]
	for _, m := range msgs {
		if w.contains(m.Ts) {
			out = append(out, m)
		}
	}
	return out
}

// filterThreadsByWindow keeps a thread whole when at least one member's ts
// lies inside the window. Members outside the window come along — that's the
// "thread completion" rule.
func filterThreadsByWindow(groups []*modelv1.ResolvedThreadFile, w readWindow) []*modelv1.ResolvedThreadFile {
	out := groups[:0:0]
	for _, g := range groups {
		if threadHasMemberInWindow(g, w) {
			out = append(out, g)
		}
	}
	return out
}

func threadHasMemberInWindow(g *modelv1.ResolvedThreadFile, w readWindow) bool {
	if g.Parent.ID != "" && w.contains(g.Parent.Ts) {
		return true
	}
	for _, r := range g.Replies {
		if w.contains(r.Ts) {
			return true
		}
	}
	return false
}

// dedupParentsInThreads drops standalone messages whose ID is the parent of
// some kept thread group. The thread atom carries the parent — keeping it
// in the standalone list too would emit the parent twice.
func dedupParentsInThreads(standalones []modelv1.ResolvedMsg, threads []*modelv1.ResolvedThreadFile) []modelv1.ResolvedMsg {
	parents := make(map[string]struct{}, len(threads))
	for _, g := range threads {
		if g.Parent.ID != "" {
			parents[g.Parent.ID] = struct{}{}
		}
	}
	if len(parents) == 0 {
		return standalones
	}
	out := standalones[:0:0]
	for _, m := range standalones {
		if _, ok := parents[m.ID]; ok {
			continue
		}
		out = append(out, m)
	}
	return out
}

// interleaveAtoms sorts standalone messages and thread atoms by their sort-ts
// and emits a flat list. Inside each thread atom, parent comes first followed
// by replies in ts order.
func interleaveAtoms(standalones []modelv1.ResolvedMsg, threads []*modelv1.ResolvedThreadFile) []modelv1.ResolvedMsg {
	type atom struct {
		ts     time.Time
		msg    *modelv1.ResolvedMsg
		thread *modelv1.ResolvedThreadFile
	}
	atoms := make([]atom, 0, len(standalones)+len(threads))
	for i := range standalones {
		atoms = append(atoms, atom{ts: standalones[i].Ts, msg: &standalones[i]})
	}
	for _, g := range threads {
		atoms = append(atoms, atom{ts: threadSortTs(g), thread: g})
	}
	sort.SliceStable(atoms, func(i, j int) bool {
		return atoms[i].ts.Before(atoms[j].ts)
	})

	var result []modelv1.ResolvedMsg
	for _, a := range atoms {
		if a.msg != nil {
			result = append(result, *a.msg)
			continue
		}
		g := a.thread
		if g.Parent.ID != "" {
			result = append(result, g.Parent)
		}
		replies := append([]modelv1.ResolvedMsg(nil), g.Replies...)
		sort.SliceStable(replies, func(i, j int) bool {
			return replies[i].Ts.Before(replies[j].Ts)
		})
		for _, r := range replies {
			r.Reply = true
			result = append(result, r)
		}
	}
	return result
}

// threadSortTs is the sort key for a thread atom — the parent's ts when
// the parent line was written, or the earliest reply's ts for orphans.
func threadSortTs(g *modelv1.ResolvedThreadFile) time.Time {
	if g.Parent.ID != "" {
		return g.Parent.Ts
	}
	var earliest time.Time
	for _, r := range g.Replies {
		if earliest.IsZero() || r.Ts.Before(earliest) {
			earliest = r.Ts
		}
	}
	return earliest
}
