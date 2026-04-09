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

// CurrentUser returns the email of the account currently logged into the
// gws CLI, or an empty string if gws is not authenticated. An error is
// returned only when gws is missing from PATH or emits output that cannot
// be parsed — "not logged in" is a normal state, not an error.
func CurrentUser() (string, error) {
	cmd := exec.Command("gws", "auth", "status")
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// Non-zero exit from `gws auth status` means "not logged in"
			// (or credentials are invalid). Either way, from pigeon's
			// perspective there is no current user — not an error.
			return "", nil
		}
		return "", fmt.Errorf("run gws auth status: %w", err)
	}

	var s struct {
		User string `json:"user"`
	}
	if err := json.Unmarshal(out, &s); err != nil {
		return "", fmt.Errorf("parse gws auth status: %w", err)
	}
	return s.User, nil
}
