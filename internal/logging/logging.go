// Package logging provides shared log initialization for pigeon processes.
package logging

import (
	"log/slog"
	"os"
	"path/filepath"

	"gopkg.in/natefinch/lumberjack.v2"
)

// InitFile configures the default slog logger to write to a rotating log file.
// It ensures the parent directory exists before opening the file.
func InitFile(filename string, maxSizeMB, maxBackups int) {
	os.MkdirAll(filepath.Dir(filename), 0755)

	w := &lumberjack.Logger{
		Filename:   filename,
		MaxSize:    maxSizeMB,
		MaxBackups: maxBackups,
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))
}
