package identity

import (
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
	norm := NormalizeEmail(email)
	return slices.ContainsFunc(p.Email, func(e string) bool {
		return NormalizeEmail(e) == norm
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
