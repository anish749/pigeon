// Package tailapi defines the wire contract for the daemon's /api/tail
// streaming endpoint. Both the daemon handler (internal/hub) and the
// CLI's monitor command (internal/commands) import this package so the
// request type and its serialization live in exactly one place.
package tailapi

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	"github.com/anish749/pigeon/internal/account"
)

// Request is the set of inputs a client sends to /api/tail.
// All fields are optional. An empty Request means "all accounts, live only".
type Request struct {
	// Accounts filters events to these accounts. Empty = no filter.
	Accounts []account.Account `json:"accounts,omitempty"`

	// Since replays events recorded at or after this time before
	// streaming live. Zero = live only, no replay.
	Since time.Time `json:"since,omitempty"`
}

// QueryParam is the single URL query key that carries the encoded
// Request. Kept as a constant so client and server can't drift.
const QueryParam = "q"

// Encode serializes a Request into query parameters. The payload is
// base64(json) under a single key so URL encoding stays boring and
// adding fields never requires new parsing on either side.
func (r Request) Encode() (url.Values, error) {
	q := url.Values{}
	if r.IsZero() {
		return q, nil
	}
	payload, err := json.Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("marshal tail request: %w", err)
	}
	q.Set(QueryParam, base64.RawURLEncoding.EncodeToString(payload))
	return q, nil
}

// Decode reads the query parameters written by Encode and returns the
// Request. A missing query key returns a zero Request (no filter, live only).
func Decode(q url.Values) (Request, error) {
	raw := q.Get(QueryParam)
	if raw == "" {
		return Request{}, nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return Request{}, fmt.Errorf("decode tail request: %w", err)
	}
	var r Request
	if err := json.Unmarshal(payload, &r); err != nil {
		return Request{}, fmt.Errorf("unmarshal tail request: %w", err)
	}
	return r, nil
}

// IsZero reports whether the request carries any filter or replay input.
func (r Request) IsZero() bool {
	return len(r.Accounts) == 0 && r.Since.IsZero()
}
