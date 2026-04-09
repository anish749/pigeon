package poller_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/gws/gwsstore"
	"github.com/anish749/pigeon/internal/gws/poller"
	"github.com/anish749/pigeon/internal/paths"
)

// TestLiveSmoke runs a real seed+poll cycle against the Google APIs.
// Requires gws CLI to be authenticated. Skip in CI.
//
// Run with: GWS_LIVE_TEST=1 go test ./internal/gws/poller/ -run TestLiveSmoke -v -timeout 60s
func TestLiveSmoke(t *testing.T) {
	if os.Getenv("GWS_LIVE_TEST") == "" {
		t.Skip("set GWS_LIVE_TEST=1 to run live smoke test")
	}

	account := paths.NewDataRoot(t.TempDir()).Platform("gws").AccountFromSlug("test")
	accountDir := account.Path()
	cursorsPath := account.SyncCursorsPath()

	cursors, err := gwsstore.LoadCursors(cursorsPath)
	if err != nil {
		t.Fatalf("load cursors: %v", err)
	}

	// --- Seed all three services ---
	t.Log("=== Seeding Gmail ===")
	if _, err := poller.PollGmail(account, cursors); err != nil {
		t.Fatalf("gmail seed: %v", err)
	}
	if cursors.Gmail.HistoryID == "" {
		t.Fatal("gmail historyId not seeded")
	}
	t.Logf("gmail historyId: %s", cursors.Gmail.HistoryID)

	t.Log("=== Seeding Calendar ===")
	if _, err := poller.PollCalendar(account, cursors); err != nil {
		t.Fatalf("calendar seed: %v", err)
	}
	if cursors.Calendar["primary"] == nil || cursors.Calendar["primary"].SyncToken == "" {
		t.Fatal("calendar syncToken not seeded")
	}
	t.Logf("calendar syncToken: %.20s...", cursors.Calendar["primary"].SyncToken)

	t.Log("=== Seeding Drive ===")
	if _, err := poller.PollDrive(account, cursors); err != nil {
		t.Fatalf("drive seed: %v", err)
	}
	if cursors.Drive.PageToken == "" {
		t.Fatal("drive pageToken not seeded")
	}
	t.Logf("drive pageToken: %s", cursors.Drive.PageToken)

	if err := gwsstore.SaveCursors(cursorsPath, cursors); err != nil {
		t.Fatalf("save cursors: %v", err)
	}

	// --- Create a test Google Doc ---
	t.Log("=== Creating test Google Doc ===")
	out, err := exec.Command("gws", "docs", "documents", "create",
		"--json", `{"title":"Pigeon Validation Test"}`).Output()
	if err != nil {
		t.Fatalf("create doc: %v", err)
	}
	// Extract documentId from JSON output.
	var docID string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, `"documentId"`) {
			docID = strings.Trim(strings.TrimPrefix(line, `"documentId": `), `",`)
			break
		}
	}
	if docID == "" {
		t.Fatalf("could not extract documentId from: %s", string(out)[:200])
	}
	t.Logf("created doc: %s", docID)
	t.Cleanup(func() {
		exec.Command("gws", "drive", "files", "delete",
			"--params", `{"fileId":"`+docID+`"}`).Run()
		t.Logf("deleted test doc %s", docID)
	})

	// Wait for Drive changes API propagation.
	t.Log("=== Waiting 3s for Drive changes propagation ===")
	time.Sleep(3 * time.Second)

	t.Log("=== Polling Drive ===")
	if _, err := poller.PollDrive(account, cursors); err != nil {
		t.Fatalf("drive poll: %v", err)
	}
	if err := gwsstore.SaveCursors(cursorsPath, cursors); err != nil {
		t.Fatalf("save cursors: %v", err)
	}

	// --- Verify files on disk ---
	t.Log("=== Files on disk ===")
	var mdFiles, metaFiles, jsonlFiles int
	filepath.Walk(accountDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(accountDir, path)
		t.Logf("  %s (%d bytes)", rel, info.Size())
		switch {
		case strings.HasSuffix(rel, ".md"):
			mdFiles++
		case strings.HasSuffix(rel, "meta.json"):
			metaFiles++
		case strings.HasSuffix(rel, ".jsonl"):
			jsonlFiles++
		}
		return nil
	})

	if mdFiles == 0 {
		t.Error("expected at least one .md file from the test doc")
	}
	if metaFiles == 0 {
		t.Error("expected at least one meta.json file")
	}

	// --- Verify .md content ---
	filepath.Walk(accountDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		data, _ := os.ReadFile(path)
		rel, _ := filepath.Rel(accountDir, path)
		t.Logf("  content of %s: %q", rel, string(data)[:min(len(data), 100)])
		return nil
	})

	// --- Verify meta.json content ---
	filepath.Walk(accountDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, "meta.json") {
			return nil
		}
		data, _ := os.ReadFile(path)
		rel, _ := filepath.Rel(accountDir, path)
		t.Logf("  content of %s: %s", rel, string(data))
		return nil
	})

	// --- Second poll (should be quiet) ---
	t.Log("=== Second poll (expect no changes) ===")
	if _, err := poller.PollGmail(account, cursors); err != nil {
		t.Errorf("gmail poll 2: %v", err)
	}
	if _, err := poller.PollCalendar(account, cursors); err != nil {
		t.Errorf("calendar poll 2: %v", err)
	}
	if _, err := poller.PollDrive(account, cursors); err != nil {
		t.Errorf("drive poll 2: %v", err)
	}
}
