package cli

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"

	"github.com/anish/claude-msg-utils/internal/logging"
	mcpserver "github.com/anish/claude-msg-utils/internal/mcp/server"
)

func newMCPCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "mcp",
		Short:   "Start the MCP server over stdio",
		GroupID: groupDaemon,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("get working directory: %w", err)
			}
			logging.InitFile(logging.MCP,
				slog.String("session_id", os.Getenv("PIGEON_SESSION_ID")),
				slog.String("cwd", cwd),
			)
			s := mcpserver.New()
			slog.Info("serving stdio")
			return server.ServeStdio(s)
		},
	}
}
