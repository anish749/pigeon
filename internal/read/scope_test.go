package read

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/workspace"
)

func makeWorkspace(name string, accounts []account.Account) *workspace.Workspace {
	return &workspace.Workspace{
		Name:     config.WorkspaceName(name),
		Accounts: accounts,
	}
}

func TestSearchDirs_NoWorkspace_NoFilters(t *testing.T) {
	dirs, err := SearchDirs(nil, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(dirs) != 1 {
		t.Fatalf("got %d dirs, want 1", len(dirs))
	}
	// Should return the data root.
	want := paths.DefaultDataRoot().Path()
	if dirs[0] != want {
		t.Errorf("got %q, want %q", dirs[0], want)
	}
}

func TestSearchDirs_NoWorkspace_PlatformFilter(t *testing.T) {
	dirs, err := SearchDirs(nil, "slack", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(dirs) != 1 {
		t.Fatalf("got %d dirs, want 1", len(dirs))
	}
	if !strings.HasSuffix(dirs[0], filepath.Join("slack")) {
		t.Errorf("got %q, want path ending in slack/", dirs[0])
	}
}

func TestSearchDirs_NoWorkspace_PlatformAndAccount(t *testing.T) {
	dirs, err := SearchDirs(nil, "slack", "acme-corp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(dirs) != 1 {
		t.Fatalf("got %d dirs, want 1", len(dirs))
	}
	if !strings.HasSuffix(dirs[0], filepath.Join("slack", "acme-corp")) {
		t.Errorf("got %q, want path ending in slack/acme-corp", dirs[0])
	}
}

func TestSearchDirs_Workspace_AllAccounts(t *testing.T) {
	ws := makeWorkspace("work", []account.Account{
		account.New("slack", "acme-corp"),
		account.New("whatsapp", "+15551234567"),
	})

	dirs, err := SearchDirs(ws, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(dirs) != 2 {
		t.Fatalf("got %d dirs, want 2", len(dirs))
	}
	if !strings.HasSuffix(dirs[0], filepath.Join("slack", "acme-corp")) {
		t.Errorf("dirs[0] = %q, want path ending in slack/acme-corp", dirs[0])
	}
}

func TestSearchDirs_Workspace_PlatformFilter(t *testing.T) {
	ws := makeWorkspace("work", []account.Account{
		account.New("slack", "acme-corp"),
		account.New("whatsapp", "+15551234567"),
		account.New("slack", "side-project"),
	})

	dirs, err := SearchDirs(ws, "slack", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(dirs) != 2 {
		t.Fatalf("got %d dirs, want 2: %v", len(dirs), dirs)
	}
	for _, d := range dirs {
		if !strings.Contains(d, "slack") {
			t.Errorf("got %q, want path containing slack", d)
		}
	}
}

func TestSearchDirs_Workspace_PlatformFilter_NoMatch(t *testing.T) {
	ws := makeWorkspace("work", []account.Account{
		account.New("slack", "acme-corp"),
	})

	_, err := SearchDirs(ws, "whatsapp", "")
	if err == nil {
		t.Fatal("expected error for platform not in workspace")
	}
}

func TestSearchDirs_Workspace_ExplicitAccountInScope(t *testing.T) {
	ws := makeWorkspace("work", []account.Account{
		account.New("slack", "acme-corp"),
		account.New("whatsapp", "+15551234567"),
	})

	dirs, err := SearchDirs(ws, "slack", "acme-corp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(dirs) != 1 {
		t.Fatalf("got %d dirs, want 1", len(dirs))
	}
	if !strings.HasSuffix(dirs[0], filepath.Join("slack", "acme-corp")) {
		t.Errorf("got %q, want path ending in slack/acme-corp", dirs[0])
	}
}

func TestSearchDirs_Workspace_ExplicitAccountOutOfScope(t *testing.T) {
	ws := makeWorkspace("work", []account.Account{
		account.New("slack", "acme-corp"),
	})

	_, err := SearchDirs(ws, "slack", "other-corp")
	if err == nil {
		t.Fatal("expected error for account not in workspace")
	}
	if !strings.Contains(err.Error(), "not in workspace") {
		t.Errorf("error = %q, want 'not in workspace'", err.Error())
	}
}

func TestSearchDirs_Workspace_EmptyWorkspace(t *testing.T) {
	ws := makeWorkspace("empty", nil)

	_, err := SearchDirs(ws, "", "")
	if err == nil {
		t.Fatal("expected error for empty workspace")
	}
}
