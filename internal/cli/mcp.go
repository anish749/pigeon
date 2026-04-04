package cli

import (
	"log/slog"

	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"

	"github.com/anish/claude-msg-utils/internal/logging"
	mcpserver "github.com/anish/claude-msg-utils/internal/mcp/server"
	"github.com/anish/claude-msg-utils/internal/paths"
)

func newMCPCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "mcp",
		Short:   "Start the MCP server over stdio",
		GroupID: groupDaemon,
		RunE: func(cmd *cobra.Command, args []string) error {
			logging.InitFile(logging.MCP)
			s := mcpserver.New(paths.SocketPath())
			slog.Info("serving stdio")
			return server.ServeStdio(s)
		},
	}
}
