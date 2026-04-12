package identity

import (
	"maps"
	"slices"
	"strings"
)

// Person represents a single person's cross-source identity.
// One JSONL line per person in people.jsonl.
type Person struct {
	Name     string                       `json:"name"`
	Email    []string                     `json:"email,omitempty"`
	Slack    map[string]PersonSlack       `json:"slack,omitempty"`
	WhatsApp []string                     `json:"whatsapp,omitempty"`
	Seen     string                       `json:"seen"` // YYYY-MM-DD
}

// PersonSlack holds a person's identity in a single Slack workspace.
type PersonSlack struct {
	ID          string `json:"id"`                    // Slack user ID (U-prefixed)
	DisplayName string `json:"displayName,omitempty"` // profile display name
	RealName    string `json:"realName,omitempty"`    // profile real name
	Name        string `json:"name,omitempty"`        // username (handle)
}

// matchesEmail reports whether this person has the given email address.
// Comparison is case-insensitive. For Gmail addresses, dots and plus
// suffixes are ignored.
func (p *Person) matchesEmail(email string) bool {
	norm := normalizeEmail(email)
	return slices.ContainsFunc(p.Email, func(e string) bool {
		return normalizeEmail(e) == norm
	})
}

// hasExactEmail reports whether this person already has the given email
// stored verbatim (case-insensitive).
func (p *Person) hasExactEmail(email string) bool {
	lower := strings.ToLower(email)
	return slices.ContainsFunc(p.Email, func(e string) bool {
		return strings.ToLower(e) == lower
	})
}

// matchesSlackID reports whether this person has the given Slack user ID
// in any workspace.
func (p *Person) matchesSlackID(id string) bool {
	for _, s := range p.Slack {
		if s.ID == id {
			return true
		}
	}
	return false
}

// matchesPhone reports whether this person has the given phone number.
func (p *Person) matchesPhone(phone string) bool {
	return slices.Contains(p.WhatsApp, phone)
}

// matchesAnyExactID reports whether q equals this person's Slack user ID (any
// workspace), a stored WhatsApp number, or an email address (email comparison
// is case-insensitive).
func (p *Person) matchesAnyExactID(q string) bool {
	if q == "" {
		return false
	}
	if p.matchesSlackID(q) {
		return true
	}
	if p.matchesPhone(q) {
		return true
	}
	return p.hasExactEmail(q)
}

// nameMatchesSubstring reports whether q matches Person.Name or any Slack
// display name, real name, or username (case-insensitive substring).
func (p *Person) nameMatchesSubstring(q string) bool {
	if q == "" {
		return false
	}
	ql := strings.ToLower(q)
	if strings.Contains(strings.ToLower(p.Name), ql) {
		return true
	}
	for _, s := range p.Slack {
		if strings.Contains(strings.ToLower(s.DisplayName), ql) {
			return true
		}
		if strings.Contains(strings.ToLower(s.RealName), ql) {
			return true
		}
		if strings.Contains(strings.ToLower(s.Name), ql) {
			return true
		}
	}
	return false
}

// merge applies a signal's identifiers to this person, adding any new
// information without removing existing data.
func (p *Person) merge(sig Signal, today string) {
	// Update name if the signal provides one and this person has none,
	// or if the signal comes from a richer source. For simplicity we
	// always take a non-empty name — the caller controls signal
	// priority by ordering.
	if sig.Name != "" {
		p.Name = sig.Name
	}

	if sig.Email != "" && !p.hasExactEmail(sig.Email) {
		p.Email = append(p.Email, sig.Email)
	}

	if sig.Slack != nil {
		if p.Slack == nil {
			p.Slack = make(map[string]PersonSlack)
		}
		p.Slack[sig.Slack.Workspace] = PersonSlack{
			ID:          sig.Slack.ID,
			DisplayName: sig.Slack.DisplayName,
			RealName:    sig.Slack.RealName,
			Name:        sig.Slack.Name,
		}
	}

	if sig.Phone != "" && !p.matchesPhone(sig.Phone) {
		p.WhatsApp = append(p.WhatsApp, sig.Phone)
	}

	if today > p.Seen {
		p.Seen = today
	}
}

// searchCandidates returns people matching the trimmed query. If the query
// equals a stable identifier (Slack user ID, email, or phone), at most one
// person is returned. Otherwise names are matched case-insensitively.
func searchCandidates(people []Person, query string) []Person {
	q := strings.TrimSpace(strings.TrimPrefix(query, "@"))
	if q == "" {
		return nil
	}
	for i := range people {
		if people[i].matchesAnyExactID(q) {
			p := people[i]
			return []Person{p}
		}
	}
	var out []Person
	for i := range people {
		if people[i].nameMatchesSubstring(q) {
			p := people[i]
			out = append(out, p)
		}
	}
	return out
}

// findMatch returns the index of the first person that matches the signal
// by a stable identifier (email, Slack user ID, or phone). Returns -1 if
// no match.
func findMatch(people []Person, sig Signal) int {
	for i := range people {
		if sig.Email != "" && people[i].matchesEmail(sig.Email) {
			return i
		}
		if sig.Slack != nil && people[i].matchesSlackID(sig.Slack.ID) {
			return i
		}
		if sig.Phone != "" && people[i].matchesPhone(sig.Phone) {
			return i
		}
	}
	return -1
}

// findPersonMatch returns the index of the first person in `people` that
// shares any stable identifier (email, Slack ID in any workspace, or phone)
// with `q`. Used by the Reader to merge across per-source files.
func findPersonMatch(people []Person, q Person) int {
	for i := range people {
		for _, e := range q.Email {
			if people[i].matchesEmail(e) {
				return i
			}
		}
		for _, s := range q.Slack {
			if s.ID != "" && people[i].matchesSlackID(s.ID) {
				return i
			}
		}
		for _, p := range q.WhatsApp {
			if people[i].matchesPhone(p) {
				return i
			}
		}
	}
	return -1
}

// mergePerson folds src into dst and returns the result. Union of all
// identifiers; on conflicting fields (name, same-workspace Slack entry),
// the more recently-seen record wins.
func mergePerson(dst, src Person) Person {
	srcNewer := src.Seen > dst.Seen
	dst.Slack = maps.Clone(dst.Slack)

	if src.Name != "" && (dst.Name == "" || srcNewer) {
		dst.Name = src.Name
	}

	for _, e := range src.Email {
		if !dst.hasExactEmail(e) {
			dst.Email = append(dst.Email, e)
		}
	}

	if src.Slack != nil {
		if dst.Slack == nil {
			dst.Slack = make(map[string]PersonSlack)
		}
		for ws, s := range src.Slack {
			if _, exists := dst.Slack[ws]; !exists || srcNewer {
				dst.Slack[ws] = s
			}
		}
	}

	for _, p := range src.WhatsApp {
		if !dst.matchesPhone(p) {
			dst.WhatsApp = append(dst.WhatsApp, p)
		}
	}

	if src.Seen > dst.Seen {
		dst.Seen = src.Seen
	}

	return dst
}

// newPerson creates a new Person from a signal.
func newPerson(sig Signal, today string) Person {
	p := Person{
		Name: sig.Name,
		Seen: today,
	}
	if sig.Email != "" {
		p.Email = []string{sig.Email}
	}
	if sig.Slack != nil {
		p.Slack = map[string]PersonSlack{
			sig.Slack.Workspace: {
				ID:          sig.Slack.ID,
				DisplayName: sig.Slack.DisplayName,
				RealName:    sig.Slack.RealName,
				Name:        sig.Slack.Name,
			},
		}
	}
	if sig.Phone != "" {
		p.WhatsApp = []string{sig.Phone}
	}
	return p
}
