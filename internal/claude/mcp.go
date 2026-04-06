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
	cli := &execCLI{claudePath: claudePath}
	return ensureMCP(cli, pigeonPath)
}

// claudeCLI abstracts the claude mcp subcommands for testing.
type claudeCLI interface {
	MCPGet(name string) (string, error)
	MCPRemove(name string) error
	MCPAdd(name, command string, args ...string) error
}

func ensureMCP(cli claudeCLI, pigeonPath string) error {
	output, err := cli.MCPGet(mcpServerName)
	if err != nil {
		// Not registered — add it.
		return cli.MCPAdd(mcpServerName, pigeonPath, "mcp")
	}

	if parseMCPField(output, "Command:") == pigeonPath && parseMCPField(output, "Args:") == "mcp" {
		return nil
	}

	// Stale config — remove and re-add.
	if err := cli.MCPRemove(mcpServerName); err != nil {
		return err
	}
	return cli.MCPAdd(mcpServerName, pigeonPath, "mcp")
}

// execCLI implements claudeCLI by shelling out to the real claude binary.
type execCLI struct {
	claudePath string
}

func (c *execCLI) MCPGet(name string) (string, error) {
	out, err := exec.Command(c.claudePath, "mcp", "get", name).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("mcp get %s: %s", name, out)
	}
	return string(out), nil
}

func (c *execCLI) MCPRemove(name string) error {
	out, err := exec.Command(c.claudePath, "mcp", "remove", name, "-s", "user").CombinedOutput()
	if err != nil {
		return fmt.Errorf("remove MCP server: %s", out)
	}
	return nil
}

func (c *execCLI) MCPAdd(name, command string, args ...string) error {
	cmdArgs := []string{"mcp", "add", "--transport", "stdio", "--scope", "user", name, "--", command}
	cmdArgs = append(cmdArgs, args...)
	out, err := exec.Command(c.claudePath, cmdArgs...).CombinedOutput()
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
