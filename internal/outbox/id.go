package outbox

import "crypto/rand"

// shortID generates a 6-character alphanumeric ID.
func shortID() string {
	return rand.Text()[:6]
}
