package main

import (
	"log/slog"
	"os"
	"path/filepath"

	"github.com/mark3labs/mcp-go/server"
	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/anish/claude-msg-utils/internal/daemon"
	mcpserver "github.com/anish/claude-msg-utils/internal/mcp/server"
)

func main() {
	initLogging()

	s := mcpserver.New(daemon.SocketPath())

	slog.Info("serving stdio")
	if err := server.ServeStdio(s); err != nil {
		slog.Error("serve error", "error", err)
		os.Exit(1)
	}
}

func initLogging() {
	stateDir := os.Getenv("PIGEON_STATE_DIR")
	if stateDir == "" {
		home, _ := os.UserHomeDir()
		stateDir = filepath.Join(home, ".local", "state", "pigeon")
	}
	os.MkdirAll(stateDir, 0755)

	w := &lumberjack.Logger{
		Filename:   filepath.Join(stateDir, "mcp.log"),
		MaxSize:    5,
		MaxBackups: 1,
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))
}
