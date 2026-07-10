package commands

import (
	"strings"
	"testing"
)

func TestRunSend_RejectsEmptyMention(t *testing.T) {
	// Validation must fire before any daemon call, so no daemon is needed.
	err := RunSend(SendParams{
		Platform: "slack",
		Account:  "acme-corp",
		Channel:  "#eng",
		Message:  "<@> deploy done",
	})
	if err == nil {
		t.Fatal("empty mention should be rejected")
	}
	if !strings.Contains(err.Error(), "empty mention") {
		t.Errorf("unexpected error: %v", err)
	}
}
