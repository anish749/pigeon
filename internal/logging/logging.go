// Package logging provides shared log initialization for pigeon processes.
package logging

import (
	"log/slog"
	"os"
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
