package cli

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/anish749/pigeon/internal/account"
	daemonclient "github.com/anish749/pigeon/internal/daemon/client"
	"github.com/anish749/pigeon/internal/timeutil"
)

func newMonitorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "monitor",
		Short:   "Stream incoming messages to stdout (peek-only)",
		GroupID: groupReading,
		Long: `Stream incoming messages and reactions as JSON lines to stdout.

The output is suitable for piping into tools like jq or grep --line-buffered.
Each line is a single event. Designed to be run under Claude Code's Monitor
tool.

No cursor is maintained — running monitor does not affect MCP channel
delivery to any Claude Code session. Safe to run alongside ` + "`pigeon claude`" + `.`,
		Example: `  pigeon monitor
  pigeon monitor --platform=slack
  pigeon monitor --platform=slack --account=acme-corp
  pigeon monitor --workspace=eng
  pigeon monitor --since=5m
  pigeon monitor | jq -r '.content'`,
		PreRunE: ensureDaemon,
		RunE:    runMonitor,
	}
	cmd.Flags().StringP("platform", "p", "", "filter by platform (slack, whatsapp)")
	cmd.Flags().StringP("account", "a", "", "filter by account (requires --platform)")
	cmd.Flags().String("since", "", "replay history from this duration ago (e.g. 5m, 2h)")
	return cmd
}

func runMonitor(cmd *cobra.Command, _ []string) error {
	platform, _ := cmd.Flags().GetString("platform")
	acctName, _ := cmd.Flags().GetString("account")
	since, _ := cmd.Flags().GetString("since")

	q := url.Values{}

	// Workspace resolution happens at the CLI layer. If the user set
	// --workspace or PIGEON_WORKSPACE, activeWorkspace is populated;
	// we expand it into the accounts= list here and send that to the
	// daemon. If the user also passed --platform/--account, those
	// override the workspace list.
	switch {
	case acctName != "" && platform != "":
		if activeWorkspace != nil {
			if err := validateAccountInWorkspace(account.New(platform, acctName)); err != nil {
				return err
			}
		}
		q.Set("accounts", platform+":"+acctName)
	case platform != "" && acctName == "":
		accts := scopedAccounts(platform)
		if len(accts) == 0 {
			return fmt.Errorf("no configured accounts for platform %q", platform)
		}
		q.Set("accounts", joinAccounts(accts))
	case activeWorkspace != nil:
		q.Set("accounts", joinAccounts(activeWorkspace.Accounts))
	}

	if since != "" {
		d, err := timeutil.ParseDuration(since)
		if err != nil {
			return fmt.Errorf("invalid --since value %q: %w", since, err)
		}
		q.Set("since", time.Now().Add(-d).UTC().Format(time.RFC3339))
	}

	reqURL := "http://pigeon/api/tail"
	if encoded := q.Encode(); encoded != "" {
		reqURL += "?" + encoded
	}

	req, err := http.NewRequestWithContext(cmd.Context(), "GET", reqURL, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	resp, err := daemonclient.DefaultPgnHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("daemon returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		fmt.Fprintln(out, strings.TrimPrefix(line, "data: "))
		out.Flush()
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read stream: %w", err)
	}
	return nil
}

// scopedAccounts returns the configured accounts for the given platform,
// limited to the active workspace if one is set.
func scopedAccounts(platform string) []account.Account {
	if activeWorkspace == nil {
		return nil
	}
	return activeWorkspace.AccountsForPlatform(platform)
}

func joinAccounts(accts []account.Account) string {
	parts := make([]string, 0, len(accts))
	for _, a := range accts {
		parts = append(parts, a.Platform+":"+a.Name)
	}
	return strings.Join(parts, ",")
}
