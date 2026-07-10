package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/anish749/pigeon/internal/identity"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/read"
	"github.com/anish749/pigeon/internal/search"
	"github.com/anish749/pigeon/internal/store"
	"github.com/anish749/pigeon/internal/workspace"
)

// ErrAmbiguous marks whois failures where the query matched more than one
// identity. The CLI maps it to exit code 2 so shell callers can distinguish
// "pick one and retry" from "not found".
var ErrAmbiguous = errors.New("ambiguous")

// WhoisParams configures a whois lookup.
type WhoisParams struct {
	Query    string
	Platform string
	Account  string
	Since    time.Duration // activity window
	IDOnly   bool          // print a single Slack user ID instead of JSONL
}

// WhoisResult is one matched person with activity in the window.
type WhoisResult struct {
	identity.Person
	Activity WhoisActivity `json:"activity"`
}

// WhoisActivity summarizes a person's synced activity within the window.
type WhoisActivity struct {
	LastActive    string   `json:"lastActive,omitempty"` // RFC 3339 UTC of most recent event
	Events        int      `json:"events"`               // messages and emails authored
	Conversations []string `json:"conversations,omitempty"`

	lastActive time.Time
}

// RunWhois looks up people across the scoped accounts' identity files and
// prints one JSONL line per match, most recently active first. With IDOnly,
// it prints the single Slack user ID of the single match, or fails with
// ErrAmbiguous when the query or the workspace is not unique.
func RunWhois(ws *workspace.Workspace, p WhoisParams, stdout, stderr io.Writer) error {
	if p.IDOnly && p.Platform != "" && p.Platform != "slack" {
		return fmt.Errorf("--id is only supported for slack")
	}
	// SearchDirs ignores the account filter when no platform is set, which
	// would make --account a silent no-op.
	if p.Account != "" && p.Platform == "" {
		if !p.IDOnly {
			return fmt.Errorf("--account requires --platform")
		}
		// --id is slack-only, so a bare --account names a slack workspace.
		p.Platform = "slack"
	}

	dirs, err := read.SearchDirs(ws, p.Platform, p.Account)
	if err != nil {
		return err
	}

	st := store.NewFSStore(paths.DefaultDataRoot())
	people, err := identity.NewReaderForRoots(st, dirs).SearchCandidates(p.Query)
	if err != nil {
		return err
	}
	if len(people) == 0 {
		return fmt.Errorf("no person matching %q", p.Query)
	}

	if p.IDOnly && len(people) == 1 {
		return printSlackID(people[0], stdout)
	}

	results := make([]WhoisResult, len(people))
	var errs []error
	for i, person := range people {
		act, err := whoisActivity(dirs, person, p.Since, stderr)
		if err != nil {
			errs = append(errs, fmt.Errorf("activity for %s: %w", person.Name, err))
		}
		results[i] = WhoisResult{Person: person, Activity: act}
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}

	// Most recently active first; inactive people ordered by last identity
	// observation.
	sort.Slice(results, func(i, j int) bool {
		a, b := results[i].Activity.lastActive, results[j].Activity.lastActive
		if !a.Equal(b) {
			return a.After(b)
		}
		return results[i].Seen > results[j].Seen
	})

	if p.IDOnly {
		return fmt.Errorf("%w: %s", ErrAmbiguous, ambiguousPeopleDetail(p.Query, results))
	}

	enc := json.NewEncoder(stdout)
	for _, r := range results {
		if err := enc.Encode(r); err != nil {
			return fmt.Errorf("encode result: %w", err)
		}
	}
	return nil
}

// printSlackID prints the person's single in-scope Slack user ID. Entries
// without a user ID don't count. A person with Slack IDs in multiple
// workspaces is ambiguous until the caller narrows the scope with --account.
func printSlackID(person identity.Person, stdout io.Writer) error {
	var workspaces []string
	for ws, s := range person.Slack {
		if s.ID != "" {
			workspaces = append(workspaces, ws)
		}
	}
	slices.Sort(workspaces)
	switch len(workspaces) {
	case 0:
		return fmt.Errorf("%s has no slack user ID in scope", person.Name)
	case 1:
		fmt.Fprintln(stdout, person.Slack[workspaces[0]].ID)
		return nil
	default:
		return fmt.Errorf("%w: %s has slack IDs in %d workspaces (%s) — narrow with --platform slack --account <workspace>",
			ErrAmbiguous, person.Name, len(workspaces), strings.Join(workspaces, ", "))
	}
}

// ambiguousPeopleDetail formats the candidate table shown on stderr when
// --id matches more than one person, with enough activity context to pick
// one and retry with a stable identifier.
func ambiguousPeopleDetail(query string, results []WhoisResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d people match %q:\n", len(results), query)
	for _, r := range results {
		fmt.Fprintf(&b, "  %-14s %s", stableID(r.Person), r.Name)
		if r.Activity.LastActive != "" {
			fmt.Fprintf(&b, "  last active %s, %d events", r.Activity.LastActive[:10], r.Activity.Events)
		} else {
			fmt.Fprintf(&b, "  no recent activity, seen %s", r.Seen)
		}
		b.WriteString("\n")
	}
	b.WriteString("retry with a stable identifier: pigeon whois <id> --id")
	return b.String()
}

// stableID returns one stable identifier for disambiguation output:
// a Slack user ID when present, else the first email, else a phone number.
func stableID(p identity.Person) string {
	for _, ws := range slices.Sorted(maps.Keys(p.Slack)) {
		return p.Slack[ws].ID
	}
	if len(p.Email) > 0 {
		return p.Email[0]
	}
	if len(p.WhatsApp) > 0 {
		return p.WhatsApp[0]
	}
	return "?"
}

// whoisActivity greps the scoped dirs for events authored by any of the
// person's identifiers within the window. WhatsApp messages carry LID JIDs
// in their from field, which people.jsonl does not store yet (see
// issues/bugs.md), so WhatsApp activity counts as zero until the listener
// observes LIDs.
func whoisActivity(dirs []string, person identity.Person, since time.Duration, stderr io.Writer) (WhoisActivity, error) {
	ids := identifiers(person)
	if len(ids) == 0 {
		return WhoisActivity{}, nil
	}
	quoted := make([]string, len(ids))
	for i, id := range ids {
		quoted[i] = regexp.QuoteMeta(id)
	}
	pattern := `"from":"(?:` + strings.Join(quoted, "|") + `)"`

	var act WhoisActivity
	convEvents := make(map[string]int)
	root := paths.DefaultDataRoot().Path()
	for _, dir := range dirs {
		out, err := read.Grep(dir, read.GrepOpts{
			Query:           pattern,
			Since:           since,
			CaseInsensitive: true,
			JSON:            true,
		})
		if err != nil {
			return WhoisActivity{}, err
		}
		matches, parseErr := search.ParseGrepOutput(out, dir)
		if parseErr != nil {
			fmt.Fprintf(stderr, "warning: some lines failed to parse: %v\n", parseErr)
		}
		for _, m := range matches {
			act.Events++
			if ts := m.Line.Ts(); ts.After(act.lastActive) {
				act.lastActive = ts
			}
			if conv, err := conversationLabel(root, m.FilePath); err == nil {
				convEvents[conv]++
			}
		}
	}

	if !act.lastActive.IsZero() {
		act.LastActive = act.lastActive.UTC().Format(time.RFC3339)
	}
	act.Conversations = make([]string, 0, len(convEvents))
	for conv := range convEvents {
		act.Conversations = append(act.Conversations, conv)
	}
	sort.Slice(act.Conversations, func(i, j int) bool {
		a, b := act.Conversations[i], act.Conversations[j]
		if convEvents[a] != convEvents[b] {
			return convEvents[a] > convEvents[b]
		}
		return a < b
	})
	return act, nil
}

// identifiers returns the person's identifiers as they appear in message
// from fields: Slack user IDs and email addresses. WhatsApp numbers are
// included for when LID observation lands.
func identifiers(p identity.Person) []string {
	var ids []string
	for _, ws := range slices.Sorted(maps.Keys(p.Slack)) {
		if id := p.Slack[ws].ID; id != "" {
			ids = append(ids, id)
		}
	}
	ids = append(ids, p.Email...)
	ids = append(ids, p.WhatsApp...)
	return ids
}

// conversationLabel renders a matched file's conversation as a data-root
// relative dir, e.g. "slack/acme-corp/#engineering" or "gws/work/gmail".
func conversationLabel(root, filePath string) (string, error) {
	rel, err := filepath.Rel(root, filePath)
	if err != nil {
		return "", fmt.Errorf("conversation label for %s: %w", filePath, err)
	}
	dir := filepath.Dir(rel)
	if filepath.Base(dir) == paths.ThreadsSubdir {
		dir = filepath.Dir(dir)
	}
	return dir, nil
}
