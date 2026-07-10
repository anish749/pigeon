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
		Short:   "Resolve a name or email to a person's user IDs and activity",
		GroupID: groupReading,
		Args:    cobra.ExactArgs(1),
		Long: `Searches the synced identity files (people.jsonl) and prints one JSON
line per matching person: name, emails, Slack user IDs per workspace,
WhatsApp numbers, and an "activity" block (lastActive, events, most
active conversations). Most recently active person first.

Names, handles, and emails match as case-insensitive substrings. An
exact Slack user ID, email, or phone number matches one person.

--id prints a single bare Slack user ID for command substitution. It
requires exactly one match: otherwise it prints nothing on stdout,
lists the candidates on stderr, and exits 2.

Exit codes: 0 found, 1 no match, 2 ambiguous (--id only).`,
		Example: `  pigeon whois alice
  pigeon whois alice@example.com | jq -r '.slack | keys[]'
  pigeon whois U012ABC3DE
  pigeon whois alice --id -p slack -a acme-corp
  pigeon send slack -a acme-corp --channel "#eng" -m "<@$(pigeon whois alice --id -a acme-corp)> deploy done"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Flag and args validation has already passed — every error
			// from here on is a runtime result (a lookup miss), not
			// misuse, so don't bury it under the usage text.
			cmd.SilenceUsage = true

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
