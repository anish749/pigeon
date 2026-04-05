// Package logging provides shared log initialization for pigeon processes.
package logging

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"

	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/anish/claude-msg-utils/internal/paths"
)

// LogFile identifies which log file to write to.
type LogFile int

const (
	Daemon LogFile = iota
	MCP
)

func (f LogFile) path() string {
	switch f {
	case MCP:
		return paths.MCPLogPath()
	default:
		return paths.DaemonLogPath()
	}
}

// Tail prints log file paths, then runs tail showing the last n lines
// from each file. If follow is true, keeps tailing until interrupted.
func Tail(n int, follow bool) error {
	var files []string
	for _, p := range []string{paths.DaemonLogPath(), paths.MCPLogPath()} {
		if _, err := os.Stat(p); err == nil {
			files = append(files, p)
		}
	}
	if len(files) == 0 {
		return fmt.Errorf("no log files found in %s", paths.StateDir())
	}

	for _, f := range files {
		fmt.Printf("  %s\n", f)
	}
	fmt.Println()

	args := []string{"-n", fmt.Sprintf("%d", n)}
	if follow {
		args = append(args, "-f")
	}
	args = append(args, files...)
	tail := exec.Command("tail", args...)
	tail.Stdout = os.Stdout
	tail.Stderr = os.Stderr
	return tail.Run()
}

// InitFile configures the default slog logger to write to a rotating log file.
// It ensures the parent directory exists before opening the file.
func InitFile(f LogFile) {
	filename := f.path()
	os.MkdirAll(filepath.Dir(filename), 0755)

	w := &lumberjack.Logger{
		Filename:   filename,
		MaxSize:    10,
		MaxBackups: 2,
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))
}
