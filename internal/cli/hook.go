package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	daemonclient "github.com/anish749/pigeon/internal/daemon/client"
)

// fallbackPreToolUse is the safe response when the daemon is unreachable or
// returns an error. "ask" defers to Claude Code's normal permission flow.
const fallbackPreToolUse = `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"ask","permissionDecisionReason":"pigeon unavailable"}}`

func newHookCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "hook",
		Short:  "Claude Code hook handlers",
		Hidden: true,
	}
	cmd.AddCommand(newHookPreToolUseCmd())
	return cmd
}

func newHookPreToolUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pretooluse",
		Short: "Handle PreToolUse hook from Claude Code",
		Long: `Reads hook input JSON from stdin, forwards it to the pigeon daemon
for approval, and writes the decision JSON to stdout.

On any error the command prints a safe fallback that defers to
Claude Code's normal permission prompt.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          runHookPreToolUse,
	}
}

func runHookPreToolUse(cmd *cobra.Command, args []string) error {
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprint(os.Stdout, fallbackPreToolUse)
		return nil
	}

	client := daemonclient.DefaultPgnHTTPClient
	resp, err := client.Post("http://pigeon/api/hook/pretooluse", "application/json", bytes.NewReader(input))
	if err != nil {
		fmt.Fprint(os.Stdout, fallbackPreToolUse)
		return nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprint(os.Stdout, fallbackPreToolUse)
		return nil
	}

	if resp.StatusCode != 200 || len(body) == 0 {
		fmt.Fprint(os.Stdout, fallbackPreToolUse)
		return nil
	}

	_, _ = os.Stdout.Write(body)
	return nil
}
