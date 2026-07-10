package cli

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/anish749/pigeon/internal/commands"
	"github.com/anish749/pigeon/internal/utils/timeutil"
)

func newWhoisCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "whois <query>",
		Short:   "Look up a person across synced platforms",
		GroupID: groupReading,
		Args:    cobra.ExactArgs(1),
		// A lookup miss is an expected outcome, not a usage error — don't
		// bury the "no person matching" message under the help text.
		SilenceUsage: true,
		Long: `Resolves a person across the synced identity files (people.jsonl).

The query is a name, Slack display name, handle, user ID, email, or
email fragment. Exact stable identifiers (Slack user ID, full email,
phone) return one person; anything else is a case-insensitive
substring match.

Output is one JSON line per person, most recently active first. Each
line carries the person's identifiers plus an "activity" block —
lastActive, events (messages/emails authored in the window), and their
most active conversations — so the right person is easy to pick when
names collide.

With --id, prints a single bare Slack user ID for use in command
substitution. Requires exactly one match with one Slack identity in
scope; otherwise prints nothing on stdout and lists the candidates on
stderr.

Exit codes: 0 found, 1 no match, 2 ambiguous (--id only).`,
		Example: `  pigeon whois alice
  pigeon whois alice@example.com | jq -r '.slack | keys[]'
  pigeon whois U012ABC3DE
  pigeon whois alice --id -p slack -a acme-corp
  pigeon send slack -a acme-corp --channel "#eng" -m "<@$(pigeon whois alice --id -a acme-corp)> deploy done"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			platform, err := cmd.Flags().GetString("platform")
			if err != nil {
				return fmt.Errorf("get platform flag: %w", err)
			}
			account, err := cmd.Flags().GetString("account")
			if err != nil {
				return fmt.Errorf("get account flag: %w", err)
			}
			since, err := cmd.Flags().GetString("since")
			if err != nil {
				return fmt.Errorf("get since flag: %w", err)
			}
			idOnly, err := cmd.Flags().GetBool("id")
			if err != nil {
				return fmt.Errorf("get id flag: %w", err)
			}

			if idOnly && platform != "" && platform != "slack" {
				return fmt.Errorf("--id is only supported for slack")
			}
			if idOnly && account != "" && platform == "" {
				platform = "slack"
			}

			var sinceDur time.Duration
			if since != "" {
				d, err := timeutil.ParseDuration(since)
				if err != nil {
					return fmt.Errorf("invalid --since value %q: %w", since, err)
				}
				sinceDur = d
			}

			runErr := commands.RunWhois(activeWorkspace, commands.WhoisParams{
				Query:    args[0],
				Platform: platform,
				Account:  account,
				Since:    sinceDur,
				IDOnly:   idOnly,
			}, os.Stdout, os.Stderr)
			if errors.Is(runErr, commands.ErrAmbiguous) {
				fmt.Fprintln(os.Stderr, "error:", runErr)
				os.Exit(2)
			}
			return runErr
		},
	}
	cmd.Flags().StringP("platform", "p", "", "filter by platform")
	cmd.Flags().StringP("account", "a", "", "filter by account")
	cmd.Flags().String("since", "30d", "activity window (e.g. 2h, 7d)")
	cmd.Flags().Bool("id", false, "print one bare Slack user ID; fail if not exactly one match")
	return cmd
}
