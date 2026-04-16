// Package gws shells out to the gws CLI for Google Workspace API calls.
package gws

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
)

// BackfillDays is the number of days of historical data to fetch on first sync.
// Used across all three services (Gmail, Calendar, Drive) for backfill windows.
const BackfillDays = 90

// ExpansionThresholdDays is how close expanded_until must be to now before
// we re-expand recurring events into the future.
const ExpansionThresholdDays = 30

// APIError represents a structured error from a Google Workspace API call.
// The gws CLI returns these as JSON on stderr: {"error":{"code":404,"message":"...","reason":"..."}}.
type APIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Reason  string `json:"reason"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("gws api %d %s: %s", e.Code, e.Reason, e.Message)
}

// IsStatusCode reports whether err is an APIError with the given HTTP status code.
func IsStatusCode(err error, code int) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Code == code
	}
	return false
}

// IsNotFound reports whether err is an APIError with HTTP 404.
func IsNotFound(err error) bool { return IsStatusCode(err, 404) }

// IsGone reports whether err is an APIError with HTTP 410.
func IsGone(err error) bool { return IsStatusCode(err, 410) }

// IsCursorExpired reports whether err indicates a stale sync cursor.
// Gmail returns 404 for expired historyId; Calendar returns 410 for
// expired syncToken or 400/invalid for corrupted tokens.
func IsCursorExpired(err error) bool {
	if IsNotFound(err) || IsGone(err) {
		return true
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Code == 400 && apiErr.Reason == "invalid"
	}
	return false
}

// Client wraps the gws CLI with optional per-account environment variables.
type Client struct {
	env []string // "KEY=VALUE" pairs merged onto os.Environ()
}

// NewClient creates a Client that injects the given environment variables
// into every gws subprocess. Pass nil for default (inherited) environment.
func NewClient(env map[string]string) *Client {
	var pairs []string
	for k, v := range env {
		pairs = append(pairs, k+"="+v)
	}
	return &Client{env: pairs}
}

// Run executes a gws CLI command and returns the raw JSON output.
// On failure, returns an *APIError if the CLI returned a structured error response.
func (c *Client) Run(args ...string) ([]byte, error) {
	cmd := exec.Command("gws", args...)
	if len(c.env) > 0 {
		cmd.Env = append(os.Environ(), c.env...)
	}
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, parseExitError(args, exitErr)
		}
		return nil, fmt.Errorf("gws %v: %w", args, err)
	}
	return out, nil
}

// RunParsed executes a gws CLI command and unmarshals the JSON output into dst.
func (c *Client) RunParsed(dst any, args ...string) error {
	out, err := c.Run(args...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(TrimToJSON(out), dst); err != nil {
		return fmt.Errorf("parse gws output: %w", err)
	}
	return nil
}

// TrimToJSON strips any non-JSON prefix (e.g. "Using keyring backend: keyring")
// that the gws CLI prints to stdout before its JSON payload. It returns the
// slice starting at the first '{' or '[' byte, or the original input if
// neither is found (letting the caller surface the real parse error).
// Callers that unmarshal gws.Client.Run output directly should wrap it with
// TrimToJSON; RunParsed applies it automatically.
func TrimToJSON(b []byte) []byte {
	obj := bytes.IndexByte(b, '{')
	arr := bytes.IndexByte(b, '[')
	switch {
	case obj < 0 && arr < 0:
		return b
	case obj < 0:
		return b[arr:]
	case arr < 0:
		return b[obj:]
	case obj < arr:
		return b[obj:]
	default:
		return b[arr:]
	}
}

// parseExitError attempts to extract a structured APIError from the gws CLI's
// stderr output. Falls back to a plain error if parsing fails.
func parseExitError(args []string, exitErr *exec.ExitError) error {
	stderr := exitErr.Stderr
	var envelope struct {
		Error APIError `json:"error"`
	}
	if json.Unmarshal(stderr, &envelope) == nil && envelope.Error.Code != 0 {
		return fmt.Errorf("gws %v: %w", args, &envelope.Error)
	}
	return fmt.Errorf("gws %v: %s", args, stderr)
}

// ParamsJSON marshals a map to a JSON string for --params flags.
// json.Marshal cannot fail for map[string]string — all values are valid JSON strings.
func ParamsJSON(m map[string]string) string {
	b, _ := json.Marshal(m) //nolint:errcheck // map[string]string always marshals
	return string(b)
}
