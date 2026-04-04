package main

import (
	"log/slog"
	"os"

	"github.com/mark3labs/mcp-go/server"

	"github.com/anish/claude-msg-utils/internal/logging"
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
	logging.InitFile(paths.MCPLogPath(), 5, 1)
}
