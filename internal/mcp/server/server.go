package mcpserver

import (
	"context"
	"log/slog"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	mcptool "github.com/anish/claude-msg-utils/internal/mcp/tool"
)

// New creates a configured MCP server with channel support and all tools registered.
func New() *server.MCPServer {
	hooks := &server.Hooks{}
	hooks.AddAfterInitialize(func(ctx context.Context, id any, req *mcp.InitializeRequest, result *mcp.InitializeResult) {
		result.Capabilities.Experimental = map[string]any{
			"claude/channel": map[string]any{},
		}
		ci := req.Params.ClientInfo
		slog.Info("mcp initialized", "client", ci.Name, "version", ci.Version)
	})

	s := server.NewMCPServer("pigeon", "0.1.0",
		server.WithToolCapabilities(true),
		server.WithInstructions("Pigeon MCP probe server. Use the 'probe' tool to dump environment, process, and session info. This is an experiment to validate the MCP channel protocol."),
		server.WithHooks(hooks),
	)

	mcptool.Register(s)

	return s
}

// RunCoo sends a channel notification every interval. Blocks forever.
func RunCoo(s *server.MCPServer, interval time.Duration) {
	// Wait for the stdio session to register.
	time.Sleep(3 * time.Second)

	slog.Info("coo goroutine started", "interval", interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		if err := s.SendNotificationToSpecificClient("stdio", "notifications/claude/channel", map[string]any{
			"content": "coo",
			"meta": map[string]any{
				"ts": time.Now().Format(time.RFC3339),
			},
		}); err != nil {
			slog.Error("coo notification failed", "error", err)
		} else {
			slog.Debug("coo sent")
		}
		<-ticker.C
	}
}
