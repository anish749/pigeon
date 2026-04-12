package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/anish749/pigeon/internal/commands"
)

func newReadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "read <source> [selector]",
		Short:   "Read data from a source within the active context",
		GroupID: groupReading,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 || len(args) > 2 {
				return fmt.Errorf("usage: pigeon read <source> [selector]")
			}
			return nil
		},
		Example: `  pigeon read gmail
  pigeon read gmail --since=7d
  pigeon read calendar
  pigeon read calendar secondary --date=2026-04-14
  pigeon read drive "Q2 Planning"
  pigeon read slack '#engineering' --since=2h
  pigeon read whatsapp Alice --context=personal --last=20`,
		PreRunE: ensureDaemon,
		RunE: func(cmd *cobra.Command, args []string) error {
			account, err := cmd.Flags().GetString("account")
			if err != nil {
				return err
			}
			contextName, err := cmd.Flags().GetString("context")
			if err != nil {
				return err
			}
			date, err := cmd.Flags().GetString("date")
			if err != nil {
				return err
			}
			last, err := cmd.Flags().GetInt("last")
			if err != nil {
				return err
			}
			since, err := cmd.Flags().GetString("since")
			if err != nil {
				return err
			}
			selector := ""
			if len(args) == 2 {
				selector = args[1]
			}
			return commands.RunRead(commands.ReadParams{
				Source:   args[0],
				Selector: selector,
				Account:  account,
				Context:  contextName,
				Date:     date,
				Last:     last,
				Since:    since,
			})
		},
	}
	cmd.Flags().StringP("account", "a", "", "narrow to a specific account within the resolved context")
	cmd.Flags().String("context", "", "context name overriding PIGEON_CONTEXT and default_context")
	cmd.Flags().String("date", "", "specific date (YYYY-MM-DD)")
	cmd.Flags().Int("last", 0, "show last N items where supported")
	cmd.Flags().String("since", "", "items from last duration (e.g. 30m, 2h, 7d)")
	return cmd
}
