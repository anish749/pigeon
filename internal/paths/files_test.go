package paths

import (
	"path/filepath"
	"testing"
)

func TestMetaFile(t *testing.T) {
	dir := "/data/gws/user/gdrive/my-doc-abc"
	name := "drive-meta-2026-04-07.json"
	mf := NewMetaFile(dir, name)

	if got := mf.Dir(); got != dir {
		t.Errorf("Dir() = %q, want %q", got, dir)
	}
	if got := mf.Name(); got != name {
		t.Errorf("Name() = %q, want %q", got, name)
	}
	if got, want := mf.Path(), filepath.Join(dir, name); got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
}

func TestMetaFileEmptyDir(t *testing.T) {
	// Construction with empty dir should still produce a valid path
	// (filepath.Join handles empty components).
	mf := NewMetaFile("", "meta.json")
	if got := mf.Path(); got != "meta.json" {
		t.Errorf("Path() = %q, want %q", got, "meta.json")
	}
	if got := mf.Dir(); got != "" {
		t.Errorf("Dir() = %q, want empty", got)
	}
}

func TestMetaFileZeroValue(t *testing.T) {
	// Zero value should be safe to use (no panic).
	var mf MetaFile
	if mf.Path() != "" {
		t.Errorf("zero Path() = %q, want empty", mf.Path())
	}
	if mf.Dir() != "" {
		t.Errorf("zero Dir() = %q, want empty", mf.Dir())
	}
	if mf.Name() != "" {
		t.Errorf("zero Name() = %q, want empty", mf.Name())
	}
}
