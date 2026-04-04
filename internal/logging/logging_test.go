package logging

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestLogFilePath(t *testing.T) {
	// Use a temp dir so we don't depend on the real home directory.
	tmp := t.TempDir()
	t.Setenv("PIGEON_STATE_DIR", tmp)

	tests := []struct {
		f    LogFile
		want string
	}{
		{Daemon, filepath.Join(tmp, "daemon.log")},
		{MCP, filepath.Join(tmp, "mcp.log")},
	}

	for _, tt := range tests {
		if got := tt.f.path(); got != tt.want {
			t.Errorf("LogFile(%d).path() = %q, want %q", tt.f, got, tt.want)
		}
	}
}

func TestInitFileCreatesDirectoryAndSetsLogger(t *testing.T) {
	tmp := t.TempDir()
	nested := filepath.Join(tmp, "sub", "dir")
	t.Setenv("PIGEON_STATE_DIR", nested)

	InitFile(Daemon)

	// The nested directory should have been created.
	if _, err := os.Stat(nested); err != nil {
		t.Fatalf("expected state dir to be created: %v", err)
	}

	// Write a log line and verify it lands in the file.
	slog.Info("test message", "key", "value")

	logFile := filepath.Join(nested, "daemon.log")
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("expected log file to exist: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected log file to contain data")
	}
}
