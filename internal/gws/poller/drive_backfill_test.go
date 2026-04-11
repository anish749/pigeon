package poller_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/gws/poller"
	"github.com/anish749/pigeon/internal/identity"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
)

// TestDriveBackfillLive verifies that existing Docs and Sheets are picked up
// during the Drive seed (backfill), and that incremental polling works after.
//
// Run with: GWS_LIVE_TEST=1 go test ./internal/gws/poller/ -run TestDriveBackfillLive -v -timeout 120s
func TestDriveBackfillLive(t *testing.T) {
	if os.Getenv("GWS_LIVE_TEST") == "" {
		t.Skip("set GWS_LIVE_TEST=1 to run live drive backfill test")
	}

	root := paths.NewDataRoot(t.TempDir())
	s := store.NewFSStore(root)
	id := identity.NewService(root.Identity("test").PeopleFile())
	account := root.Platform("gws").AccountFromSlug("test")

	// --- Create a test doc BEFORE seeding ---
	t.Log("=== Creating test doc before seed ===")
	docID := createDriveDoc(t, "pigeon-drive-backfill-test")
	t.Cleanup(func() { deleteDriveFile(t, docID) })

	// Wait for the file to be visible in files.list.
	t.Log("waiting 3s for API propagation")
	time.Sleep(3 * time.Second)

	// --- Phase 1: Seed with backfill ---
	t.Log("=== Phase 1: Seed with backfill ===")
	cursors, err := s.LoadGWSCursors(account)
	if err != nil {
		t.Fatalf("load cursors: %v", err)
	}

	if _, err := poller.PollDrive(s, account, cursors, id); err != nil {
		// Partial errors (e.g. formula parsing on specific sheets) are expected —
		// they don't prevent the backfill from completing.
		t.Logf("drive seed partial errors: %v", err)
	}
	if err := s.SaveGWSCursors(account, cursors); err != nil {
		t.Fatalf("save cursors: %v", err)
	}

	if cursors.Drive.PageToken == "" {
		t.Fatal("drive pageToken not seeded")
	}
	t.Logf("pageToken: %s", cursors.Drive.PageToken)

	// Verify the test doc was backfilled to disk.
	driveDir := account.Drive().Path()
	if !hasDriveFile(t, driveDir, docID) {
		t.Fatal("test doc not found on disk after backfill seed")
	}

	// Verify a drive-meta-YYYY-MM-DD.json file exists for the test doc.
	metaPath := findDriveFileMeta(t, driveDir, docID)
	if metaPath == "" {
		t.Fatal("drive-meta file not found for test doc")
	}
	t.Logf("drive-meta: %s", metaPath)

	// Verify .md content exists.
	mdPath := findDriveFileMD(t, driveDir, docID)
	if mdPath == "" {
		t.Fatal(".md file not found for test doc")
	}
	t.Logf("content: %s", mdPath)

	// --- Phase 2: Quiet incremental poll ---
	t.Log("=== Phase 2: Quiet poll ===")
	if _, err := poller.PollDrive(s, account, cursors, id); err != nil {
		t.Errorf("quiet poll: %v", err)
	}

	t.Log("=== All phases passed ===")
}

// --- helpers ---

func createDriveDoc(t *testing.T, title string) string {
	t.Helper()
	out, err := exec.Command("gws", "docs", "documents", "create",
		"--json", `{"title":"`+title+`"}`).Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			t.Fatalf("create doc: %v\nstderr: %s", err, exitErr.Stderr)
		}
		t.Fatalf("create doc: %v", err)
	}
	var resp struct {
		DocumentID string `json:"documentId"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("parse create doc response: %v\nraw: %s", err, string(out)[:min(len(out), 200)])
	}
	if resp.DocumentID == "" {
		t.Fatalf("empty documentId in response: %s", string(out)[:min(len(out), 200)])
	}
	t.Logf("created doc: %s", resp.DocumentID)
	return resp.DocumentID
}

func deleteDriveFile(t *testing.T, fileID string) {
	t.Helper()
	exec.Command("gws", "drive", "files", "delete",
		"--params", `{"fileId":"`+fileID+`"}`).Run()
	t.Logf("deleted file: %s", fileID)
}

// hasDriveFile checks if a directory for the given file ID exists under driveDir.
func hasDriveFile(t *testing.T, driveDir, fileID string) bool {
	t.Helper()
	found := false
	filepath.Walk(driveDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() && strings.Contains(info.Name(), fileID) {
			found = true
		}
		return nil
	})
	return found
}

func findDriveFileMeta(t *testing.T, driveDir, fileID string) string {
	t.Helper()
	var result string
	filepath.Walk(driveDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		base := filepath.Base(path)
		if strings.Contains(path, fileID) &&
			strings.HasPrefix(base, "drive-meta-") &&
			strings.HasSuffix(base, ".json") {
			result = path
		}
		return nil
	})
	return result
}

func findDriveFileMD(t *testing.T, driveDir, fileID string) string {
	t.Helper()
	var result string
	filepath.Walk(driveDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.Contains(path, fileID) && strings.HasSuffix(path, ".md") {
			result = path
		}
		return nil
	})
	return result
}
