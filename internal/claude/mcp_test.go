package claude

import (
	"fmt"
	"testing"
)

// fakeCLI records calls and returns canned responses for testing ensureMCP.
type fakeCLI struct {
	getOutput string // output from MCPGet, empty triggers error
	getErr    bool
	removeErr bool
	addErr    bool

	getCalled    int
	removeCalled int
	addCalled    int
	addCommand   string
	addArgs      []string
}

func (f *fakeCLI) MCPGet(name string) (string, error) {
	f.getCalled++
	if f.getErr {
		return "", fmt.Errorf("not found")
	}
	return f.getOutput, nil
}

func (f *fakeCLI) MCPRemove(name string) error {
	f.removeCalled++
	if f.removeErr {
		return fmt.Errorf("remove failed")
	}
	return nil
}

func (f *fakeCLI) MCPAdd(name, command string, args ...string) error {
	f.addCalled++
	f.addCommand = command
	f.addArgs = args
	if f.addErr {
		return fmt.Errorf("add failed")
	}
	return nil
}

const mcpGetOutput = `pigeon:
  Scope: User config (available in all your projects)
  Status: ✓ Connected
  Type: stdio
  Command: /usr/local/bin/pigeon
  Args: mcp
  Environment:

To remove this server, run: claude mcp remove "pigeon" -s user`

func TestEnsureMCP_NotRegistered_Adds(t *testing.T) {
	cli := &fakeCLI{getErr: true}

	err := ensureMCP(cli, "/usr/local/bin/pigeon")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cli.getCalled != 1 {
		t.Errorf("get called %d times, want 1", cli.getCalled)
	}
	if cli.addCalled != 1 {
		t.Errorf("add called %d times, want 1", cli.addCalled)
	}
	if cli.removeCalled != 0 {
		t.Errorf("remove called %d times, want 0", cli.removeCalled)
	}
	if cli.addCommand != "/usr/local/bin/pigeon" {
		t.Errorf("add command = %q, want /usr/local/bin/pigeon", cli.addCommand)
	}
}

func TestEnsureMCP_UpToDate_NoOp(t *testing.T) {
	cli := &fakeCLI{getOutput: mcpGetOutput}

	err := ensureMCP(cli, "/usr/local/bin/pigeon")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cli.addCalled != 0 {
		t.Errorf("add called %d times, want 0", cli.addCalled)
	}
	if cli.removeCalled != 0 {
		t.Errorf("remove called %d times, want 0", cli.removeCalled)
	}
}

func TestEnsureMCP_StaleCommand_RemovesAndReAdds(t *testing.T) {
	cli := &fakeCLI{getOutput: mcpGetOutput}

	// Pass a different path than what's in the get output.
	err := ensureMCP(cli, "/new/path/pigeon")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cli.removeCalled != 1 {
		t.Errorf("remove called %d times, want 1", cli.removeCalled)
	}
	if cli.addCalled != 1 {
		t.Errorf("add called %d times, want 1", cli.addCalled)
	}
	if cli.addCommand != "/new/path/pigeon" {
		t.Errorf("add command = %q, want /new/path/pigeon", cli.addCommand)
	}
}

func TestEnsureMCP_StaleArgs_RemovesAndReAdds(t *testing.T) {
	// Same command but different args.
	output := `pigeon:
  Command: /usr/local/bin/pigeon
  Args: serve`
	cli := &fakeCLI{getOutput: output}

	err := ensureMCP(cli, "/usr/local/bin/pigeon")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cli.removeCalled != 1 {
		t.Errorf("remove called %d times, want 1", cli.removeCalled)
	}
	if cli.addCalled != 1 {
		t.Errorf("add called %d times, want 1", cli.addCalled)
	}
}

func TestEnsureMCP_AddFails_ReturnsError(t *testing.T) {
	cli := &fakeCLI{getErr: true, addErr: true}

	err := ensureMCP(cli, "/usr/local/bin/pigeon")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestEnsureMCP_RemoveFails_ReturnsError(t *testing.T) {
	cli := &fakeCLI{getOutput: mcpGetOutput, removeErr: true}

	err := ensureMCP(cli, "/new/path/pigeon")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if cli.addCalled != 0 {
		t.Errorf("add should not be called when remove fails, called %d times", cli.addCalled)
	}
}

func TestParseMCPField(t *testing.T) {
	tests := []struct {
		name   string
		output string
		field  string
		want   string
	}{
		{"command", mcpGetOutput, "Command:", "/usr/local/bin/pigeon"},
		{"args", mcpGetOutput, "Args:", "mcp"},
		{"scope", mcpGetOutput, "Scope:", "User config (available in all your projects)"},
		{"missing field", mcpGetOutput, "Missing:", ""},
		{"empty output", "", "Command:", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseMCPField(tt.output, tt.field)
			if got != tt.want {
				t.Errorf("parseMCPField(%q) = %q, want %q", tt.field, got, tt.want)
			}
		})
	}
}
