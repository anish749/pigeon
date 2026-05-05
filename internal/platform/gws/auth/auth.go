// Package gwsauth reports the Google Workspace CLI's current login state.
//
// Pigeon does not own Google Workspace authentication — the external `gws`
// CLI does. This package is a thin probe: it shells out to `gws auth status`
// and returns the email of the account currently logged in, or an empty
// string if no one is logged in. Scopes, tokens, and the OAuth flow are all
// handled by the `gws` CLI itself.
package gwsauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
)

// User is the account currently logged into the gws CLI.
type User struct {
	Email string
}

// CurrentUser returns the account currently logged into the gws CLI, or
// nil if gws is not authenticated. An error is returned only when gws is
// missing from PATH or emits output that cannot be parsed — "not logged in"
// is a normal state, not an error.
func CurrentUser() (*User, error) {
	cmd := exec.Command("gws", "auth", "status")
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// Non-zero exit from `gws auth status` means "not logged in"
			// (or credentials are invalid). Either way, from pigeon's
			// perspective there is no current user — not an error.
			return nil, nil
		}
		return nil, fmt.Errorf("run gws auth status: %w", err)
	}

	var s struct {
		User string `json:"user"`
	}
	if err := json.Unmarshal(out, &s); err != nil {
		return nil, fmt.Errorf("parse gws auth status: %w", err)
	}
	if s.User == "" {
		return nil, nil
	}
	return &User{Email: s.User}, nil
}
