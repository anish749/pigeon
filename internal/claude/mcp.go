package claude

import (
	"fmt"
	"os/exec"
	"strings"
)

const mcpServerName = "pigeon"

// EnsureMCPServer registers pigeon as a user-scoped MCP server in Claude Code.
// If the server already exists with the correct command, this is a no-op.
// If it exists with a stale command (e.g. old binary path), it is replaced.
func EnsureMCPServer(claudePath, pigeonPath string) error {
	out, err := exec.Command(claudePath, "mcp", "get", mcpServerName).CombinedOutput()
	if err != nil {
		return addMCPServer(claudePath, pigeonPath)
	}

	output := string(out)
	if parseMCPField(output, "Command:") == pigeonPath && parseMCPField(output, "Args:") == "mcp" {
		return nil
	}

	// Stale config — remove and re-add.
	if rmOut, err := exec.Command(claudePath, "mcp", "remove", mcpServerName, "-s", "user").CombinedOutput(); err != nil {
		return fmt.Errorf("remove stale MCP server: %s", rmOut)
	}
	return addMCPServer(claudePath, pigeonPath)
}

func addMCPServer(claudePath, pigeonPath string) error {
	out, err := exec.Command(claudePath, "mcp", "add", "--transport", "stdio", "--scope", "user", mcpServerName, "--", pigeonPath, "mcp").CombinedOutput()
	if err != nil {
		return fmt.Errorf("register MCP server: %s", out)
	}
	return nil
}

func parseMCPField(output, field string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, field) {
			return strings.TrimSpace(strings.TrimPrefix(line, field))
		}
	}
	return ""
}
