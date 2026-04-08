// Package gws shells out to the gws CLI for Google Workspace API calls.
package gws

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
)

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

// Run executes a gws CLI command and returns the raw JSON output.
// On failure, returns an *APIError if the CLI returned a structured error response.
func Run(args ...string) ([]byte, error) {
	cmd := exec.Command("gws", args...)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, parseExitError(args, exitErr)
		}
		return nil, fmt.Errorf("gws %v: %w", args, err)
	}
	return out, nil
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

// RunParsed executes a gws CLI command and unmarshals the JSON output into dst.
func RunParsed(dst any, args ...string) error {
	out, err := Run(args...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(out, dst); err != nil {
		return fmt.Errorf("parse gws output: %w", err)
	}
	return nil
}

// ParamsJSON marshals a map to a JSON string for --params flags.
// json.Marshal cannot fail for map[string]string — all values are valid JSON strings.
func ParamsJSON(m map[string]string) string {
	b, _ := json.Marshal(m) //nolint:errcheck // map[string]string always marshals
	return string(b)
}
