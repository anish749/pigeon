package main

import (
	"log/slog"
	"os"

	"github.com/mark3labs/mcp-go/server"
	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/anish/claude-msg-utils/internal/paths"
	mcpserver "github.com/anish/claude-msg-utils/internal/mcp/server"
)

func main() {
	initLogging()

	s := mcpserver.New(paths.SocketPath())

	slog.Info("serving stdio")
	if err := server.ServeStdio(s); err != nil {
		slog.Error("serve error", "error", err)
		os.Exit(1)
	}
}

func initLogging() {
	os.MkdirAll(paths.StateDir(), 0755)

	w := &lumberjack.Logger{
		Filename:   paths.MCPLogPath(),
		MaxSize:    5,
		MaxBackups: 1,
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))
}
