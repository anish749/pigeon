package mcptool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Register adds all MCP tools to the server.
func Register(s *server.MCPServer) {
	s.AddTool(
		mcp.NewTool("probe",
			mcp.WithDescription("Dump all available environment and session info: env vars, working directory, PIDs, and MCP client info"),
		),
		handleProbe,
	)
}

func handleProbe(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	cwd, _ := os.Getwd()

	envMap := map[string]string{}
	for _, kv := range os.Environ() {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}

	// Get client info from the session.
	var clientInfo mcp.Implementation
	if session, ok := server.ClientSessionFromContext(ctx).(server.SessionWithClientInfo); ok {
		clientInfo = session.GetClientInfo()
	}

	result := map[string]any{
		"env":        envMap,
		"cwd":        cwd,
		"pid":        os.Getpid(),
		"ppid":       os.Getppid(),
		"clientInfo": clientInfo,
	}

	b, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal probe result: %w", err)
	}
	return mcp.NewToolResultText(string(b)), nil
}
