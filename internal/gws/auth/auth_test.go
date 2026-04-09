package gwsauth_test

import (
	"os"
	"strings"
	"testing"

	gwsauth "github.com/anish749/pigeon/internal/gws/auth"
)

// TestCurrentUserLive verifies CurrentUser returns the account logged into
// `gws auth status`. Requires gws CLI to be authenticated. Skip in CI.
//
// Run with: GWS_LIVE_TEST=1 go test ./internal/gws/auth/ -run TestCurrentUserLive -v
func TestCurrentUserLive(t *testing.T) {
	if os.Getenv("GWS_LIVE_TEST") == "" {
		t.Skip("set GWS_LIVE_TEST=1 to run live gws auth test")
	}

	user, err := gwsauth.CurrentUser()
	if err != nil {
		t.Fatalf("CurrentUser: %v", err)
	}
	if user == nil {
		t.Fatal("CurrentUser returned nil — is gws logged in? run `gws auth login`")
	}
	if !strings.Contains(user.Email, "@") {
		t.Errorf("CurrentUser returned email %q, expected an email address", user.Email)
	}
	t.Logf("current gws user: %s", user.Email)
}
