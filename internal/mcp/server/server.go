package mcpserver

import (
	"context"
	"log/slog"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// New creates a configured MCP server with channel support. The server
// connects to the pigeon daemon at socketPath to receive incoming messages
// and forward them as channel notifications to Claude Code.
func New(socketPath string) *server.MCPServer {
	var s *server.MCPServer

	hooks := &server.Hooks{}
	hooks.AddAfterInitialize(func(ctx context.Context, id any, req *mcp.InitializeRequest, result *mcp.InitializeResult) {
		result.Capabilities.Experimental = map[string]any{
			"claude/channel": map[string]any{},
		}
		ci := req.Params.ClientInfo
		slog.Info("mcp initialized", "client", ci.Name, "version", ci.Version)
	})

	s = server.NewMCPServer("pigeon", "0.1.0",
		server.WithToolCapabilities(true),
		server.WithInstructions("Pigeon MCP channel server. Receives messages from WhatsApp and Slack via the pigeon daemon and delivers them as channel notifications."),
		server.WithHooks(hooks),
	)

	// Start the daemon stream after Claude Code sends notifications/initialized,
	// ensuring the client is ready to receive channel notifications.
	s.AddNotificationHandler("notifications/initialized", func(ctx context.Context, notification mcp.JSONRPCNotification) {
		slog.Info("client initialized, starting daemon stream")
		if err := startPigeonDaemonStream(context.Background(), socketPath, func(n ClaudeChannelNotification) error {
			return s.SendNotificationToSpecificClient("stdio", "notifications/claude/channel", map[string]any{
				"content": n.Content,
				"meta":    n.Meta,
			})
		}); err != nil {
			s.SendNotificationToSpecificClient("stdio", "notifications/claude/channel", map[string]any{
				"content": "pigeon channel error: " + err.Error(),
			})
			slog.Error("failed to start daemon stream", "error", err)
		}
	})

	return s
}
