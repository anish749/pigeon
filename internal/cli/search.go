package cli

import (
	"github.com/spf13/cobra"

	"github.com/anish749/pigeon/internal/commands"
)

func newSearchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "search",
		Short:   "Search across conversations by keyword",
		GroupID: groupReading,
		Example: `  pigeon search -q "deploy"
  pigeon search -q "bug" --platform=slack --account=acme-corp
  pigeon search -q "lunch" --since=7d`,
		PreRunE: ensureDaemon,
		RunE: func(cmd *cobra.Command, args []string) error {
			query, err := cmd.Flags().GetString("query")
			if err != nil {
				return err
			}
			platform, err := cmd.Flags().GetString("platform")
			if err != nil {
				return err
			}
			account, err := cmd.Flags().GetString("account")
			if err != nil {
				return err
			}
			since, err := cmd.Flags().GetString("since")
			if err != nil {
				return err
			}
			return commands.RunSearch(commands.SearchParams{
				Query:    query,
				Platform: platform,
				Account:  account,
				Since:    since,
			})
		},
	}
	cmd.Flags().StringP("query", "q", "", "search query")
	cmd.Flags().StringP("platform", "p", "", "filter by platform")
	cmd.Flags().StringP("account", "a", "", "filter by account")
	cmd.Flags().String("since", "", "only search messages from last duration (e.g. 2h, 7d)")
	cmd.MarkFlagRequired("query")
	return cmd
}
