package api

import "strings"

// looksLikeEmail returns true if s could be an email address.
// Checks for @ not at the start and a dot after @ — the Slack API does the real validation.
func looksLikeEmail(s string) bool {
	at := strings.Index(s, "@")
	return at > 0 && strings.Contains(s[at:], ".")
}
