package gwsstore

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/anish749/pigeon/internal/paths"
)

// testDriveDir returns a DriveDir rooted at a fresh temp directory.
func testDriveDir(t *testing.T) paths.DriveDir {
	t.Helper()
	return paths.NewDataRoot(t.TempDir()).
		Platform("gws").
		AccountFromSlug("test").
		Drive()
}

func TestRemoveDriveFile_SluggedDir(t *testing.T) {
	drive := testDriveDir(t)
	target := filepath.Join(drive.Path(), "my-doc-fileID123")
	keep := filepath.Join(drive.Path(), "other-doc-fileID456")
	for _, d := range []string{target, keep} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "content.md"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := RemoveDriveFile(drive, "fileID123"); err != nil {
		t.Fatalf("RemoveDriveFile: %v", err)
	}

	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("target dir still exists: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("unrelated dir removed: %v", err)
	}
}

func TestRemoveDriveFile_PlainIDDir(t *testing.T) {
	drive := testDriveDir(t)
	target := filepath.Join(drive.Path(), "fileID789")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := RemoveDriveFile(drive, "fileID789"); err != nil {
		t.Fatalf("RemoveDriveFile: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("target dir still exists: %v", err)
	}
}

func TestRemoveDriveFile_NoMatch(t *testing.T) {
	drive := testDriveDir(t)
	keep := filepath.Join(drive.Path(), "unrelated-doc-fileIDAAA")
	if err := os.MkdirAll(keep, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := RemoveDriveFile(drive, "fileIDZZZ"); err != nil {
		t.Fatalf("RemoveDriveFile: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("unrelated dir removed: %v", err)
	}
}

func TestRemoveDriveFile_MissingDriveDir(t *testing.T) {
	// A fresh account with no gdrive directory at all.
	drive := paths.NewDataRoot(t.TempDir()).
		Platform("gws").
		AccountFromSlug("neverbackfilled").
		Drive()

	if err := RemoveDriveFile(drive, "fileIDXYZ"); err != nil {
		t.Errorf("RemoveDriveFile on missing dir: %v", err)
	}
}

func TestRemoveDriveFile_IgnoresNonDirectories(t *testing.T) {
	drive := testDriveDir(t)
	if err := os.MkdirAll(drive.Path(), 0o755); err != nil {
		t.Fatal(err)
	}
	// A stray file whose name would otherwise match the suffix pattern.
	stray := filepath.Join(drive.Path(), "stray-fileID123")
	if err := os.WriteFile(stray, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := RemoveDriveFile(drive, "fileID123"); err != nil {
		t.Fatalf("RemoveDriveFile: %v", err)
	}
	if _, err := os.Stat(stray); err != nil {
		t.Errorf("stray file incorrectly removed: %v", err)
	}
}
