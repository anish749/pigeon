package read

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/paths"
)

// setupGWSFixture creates a data tree with all GWS file types (gmail JSONL,
// calendar JSONL, and Drive content with drive-meta dates). Uses the paths
// type chain for all file locations.
func setupGWSFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	today := time.Now().UTC().Format("2006-01-02")
	old := time.Now().UTC().AddDate(0, 0, -30).Format("2006-01-02")

	root := paths.NewDataRoot(dir)
	account := root.Platform("gws").AccountFromSlug("user-at-example-com")

	// Gmail JSONL files (date-partitioned).
	writeFile(t,
		account.Gmail().DateFile(today).Path(),
		`{"type":"email","id":"E1","subject":"recent email","from":"alice@example.com"}`+"\n",
	)
	writeFile(t,
		account.Gmail().DateFile(old).Path(),
		`{"type":"email","id":"E2","subject":"old email","from":"bob@example.com"}`+"\n",
	)

	// Calendar JSONL files (date-partitioned).
	writeFile(t,
		account.Calendar("primary").DateFile(today).Path(),
		`{"type":"event","id":"EV1","summary":"recent meeting"}`+"\n",
	)

	// Drive file — recent (drive-meta dated today).
	recentDoc := account.Drive().File("recent-doc-FILEID1")
	writeFile(t,
		recentDoc.MetaFile(today).Path(),
		`{"fileId":"FILEID1","title":"Recent Doc","modifiedTime":"`+today+`T09:00:00Z"}`,
	)
	writeFile(t, recentDoc.TabFile("Tab1").Path(), "# Recent Doc\n\nThis is recent markdown content.\n")
	writeFile(t, recentDoc.CommentsFile().Path(),
		`{"type":"comment","id":"C1","content":"recent comment","author":"Alice"}`+"\n",
	)

	// Drive file — old (drive-meta dated 30 days ago).
	oldDoc := account.Drive().File("old-doc-FILEID2")
	writeFile(t,
		oldDoc.MetaFile(old).Path(),
		`{"fileId":"FILEID2","title":"Old Doc","modifiedTime":"`+old+`T09:00:00Z"}`,
	)
	writeFile(t, oldDoc.TabFile("Tab1").Path(), "# Old Doc\n\nThis is old markdown content.\n")
	writeFile(t, oldDoc.CommentsFile().Path(),
		`{"type":"comment","id":"C2","content":"old comment","author":"Bob"}`+"\n",
	)

	// Drive sheet — recent.
	recentSheet := account.Drive().File("recent-sheet-FILEID3")
	writeFile(t,
		recentSheet.MetaFile(today).Path(),
		`{"fileId":"FILEID3","title":"Recent Sheet","modifiedTime":"`+today+`T09:00:00Z"}`,
	)
	writeFile(t, recentSheet.SheetFile("Sheet1").Path(), "Name,Value\nAlice,recent data\nBob,more data\n")

	return dir
}

func TestGlob_GWS_NoSince(t *testing.T) {
	dir := setupGWSFixture(t)

	files, err := Glob(dir, 0)
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}

	// Expect: 2 gmail jsonl + 1 calendar jsonl + 1 recent md + 1 old md
	//       + 1 recent comments jsonl + 1 old comments jsonl + 1 recent csv
	//       = 8 files
	if len(files) != 8 {
		for _, f := range files {
			t.Logf("  %s", f)
		}
		t.Errorf("got %d files, want 8", len(files))
	}

	// Should include files of all three extensions.
	var mdCount, csvCount, jsonlCount int
	for _, f := range files {
		switch filepath.Ext(f) {
		case ".md":
			mdCount++
		case ".csv":
			csvCount++
		case paths.FileExt:
			jsonlCount++
		}
	}
	if mdCount == 0 {
		t.Error("no .md files returned")
	}
	if csvCount == 0 {
		t.Error("no .csv files returned")
	}
	if jsonlCount == 0 {
		t.Error("no .jsonl files returned")
	}
}

func TestGlob_GWS_SinceFiltersDriveContent(t *testing.T) {
	dir := setupGWSFixture(t)

	files, err := Glob(dir, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}

	// Recent drive content should be present; old drive content should not.
	hasRecentMD := false
	hasOldMD := false
	hasRecentCSV := false
	hasRecentGmail := false
	hasOldGmail := false

	for _, f := range files {
		switch {
		case strings.Contains(f, "recent-doc-FILEID1") && strings.HasSuffix(f, ".md"):
			hasRecentMD = true
		case strings.Contains(f, "old-doc-FILEID2") && strings.HasSuffix(f, ".md"):
			hasOldMD = true
		case strings.Contains(f, "recent-sheet-FILEID3") && strings.HasSuffix(f, ".csv"):
			hasRecentCSV = true
		case strings.Contains(f, "gmail") && strings.Contains(filepath.Base(f), time.Now().UTC().Format("2006-01-02")):
			hasRecentGmail = true
		case strings.Contains(f, "gmail") && strings.Contains(filepath.Base(f), time.Now().UTC().AddDate(0, 0, -30).Format("2006-01-02")):
			hasOldGmail = true
		}
	}

	if !hasRecentMD {
		t.Error("recent drive .md not returned")
	}
	if hasOldMD {
		t.Error("old drive .md should not be returned (drive-meta is outside window)")
	}
	if !hasRecentCSV {
		t.Error("recent drive .csv not returned")
	}
	if !hasRecentGmail {
		t.Error("recent gmail .jsonl not returned")
	}
	if hasOldGmail {
		t.Error("old gmail .jsonl should not be returned")
	}

	// Drive meta files themselves should not be in the output.
	for _, f := range files {
		if strings.Contains(filepath.Base(f), "drive-meta-") {
			t.Errorf("drive-meta file should not be in output: %s", f)
		}
	}
}

func TestGlob_GWS_SinceExcludesOldComments(t *testing.T) {
	dir := setupGWSFixture(t)

	files, err := Glob(dir, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}

	// old-doc-FILEID2/comments.jsonl should NOT be returned because the
	// sibling drive-meta file's date (30 days ago) is outside the window.
	for _, f := range files {
		if strings.Contains(f, "old-doc-FILEID2") && strings.HasSuffix(f, "comments.jsonl") {
			t.Errorf("old drive comments should not be returned: %s", f)
		}
	}
}

func TestGrep_GWS_SearchesMarkdown(t *testing.T) {
	dir := setupGWSFixture(t)

	out, err := Grep(dir, GrepOpts{Query: "recent markdown"})
	if err != nil {
		t.Fatalf("Grep: %v", err)
	}
	if len(out) == 0 {
		t.Error("Grep should find markdown content")
	}
	if !strings.Contains(string(out), ".md") {
		t.Errorf("Grep should return .md path, got: %s", string(out))
	}
}

func TestGrep_GWS_SearchesCSV(t *testing.T) {
	dir := setupGWSFixture(t)

	out, err := Grep(dir, GrepOpts{Query: "recent data"})
	if err != nil {
		t.Fatalf("Grep: %v", err)
	}
	if len(out) == 0 {
		t.Error("Grep should find CSV content")
	}
	if !strings.Contains(string(out), ".csv") {
		t.Errorf("Grep should return .csv path, got: %s", string(out))
	}
}

func TestGrep_GWS_SearchesGmailJSONL(t *testing.T) {
	dir := setupGWSFixture(t)

	out, err := Grep(dir, GrepOpts{Query: "recent email"})
	if err != nil {
		t.Fatalf("Grep: %v", err)
	}
	if len(out) == 0 {
		t.Error("Grep should find gmail content")
	}
}

func TestGrep_GWS_SinceExcludesOldDriveContent(t *testing.T) {
	dir := setupGWSFixture(t)

	// "old markdown content" only appears in the old drive file.
	// With --since=7d, it should not be searched.
	out, err := Grep(dir, GrepOpts{Query: "old markdown content", Since: 7 * 24 * time.Hour})
	if err != nil {
		t.Fatalf("Grep: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("Grep --since=7d should not search old drive content, got: %s", string(out))
	}
}

func TestGrep_GWS_SinceIncludesRecentDriveContent(t *testing.T) {
	dir := setupGWSFixture(t)

	out, err := Grep(dir, GrepOpts{Query: "recent markdown content", Since: 7 * 24 * time.Hour})
	if err != nil {
		t.Fatalf("Grep: %v", err)
	}
	if len(out) == 0 {
		t.Error("Grep --since=7d should find recent drive content")
	}
}

func TestExpandDriveMetaMatches(t *testing.T) {
	dir := t.TempDir()
	today := time.Now().UTC().Format("2006-01-02")

	root := paths.NewDataRoot(dir)
	driveFile := root.Platform("gws").AccountFromSlug("test").Drive().File("my-doc-FILEID")
	writeFile(t, driveFile.MetaFile(today).Path(), `{"fileId":"FILEID"}`)
	writeFile(t, driveFile.TabFile("Tab1").Path(), "content")
	writeFile(t, driveFile.SheetFile("Sheet1").Path(), "a,b,c")
	writeFile(t, driveFile.CommentsFile().Path(), `{"type":"comment"}`)
	// Non-content file that should be ignored.
	writeFile(t, filepath.Join(driveFile.Path(), "ignore.txt"), "ignored")

	meta, ok, err := paths.ParseDriveMetaPath(driveFile.MetaFile(today).Path())
	if err != nil {
		t.Fatalf("ParseDriveMetaPath: %v", err)
	}
	if !ok {
		t.Fatal("ParseDriveMetaPath: ok=false, want true")
	}
	content, err := expandDriveMetaMatches([]paths.DriveMetaFile{meta})
	if err != nil {
		t.Fatalf("expandDriveMetaMatches: %v", err)
	}
	if len(content) != 3 {
		t.Errorf("expected 3 content files, got %d: %v", len(content), content)
	}

	hasExt := func(ext string) bool {
		for _, f := range content {
			if filepath.Ext(f) == ext {
				return true
			}
		}
		return false
	}
	if !hasExt(".md") {
		t.Error("missing .md")
	}
	if !hasExt(".csv") {
		t.Error("missing .csv")
	}
	if !hasExt(paths.FileExt) {
		t.Error("missing .jsonl")
	}
}
