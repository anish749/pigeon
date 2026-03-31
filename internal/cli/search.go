package cli

import (
	"github.com/spf13/cobra"

	"github.com/anish/claude-msg-utils/internal/commands"
)

var searchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search across conversations by keyword",
	Example: `  pigeon search -q "deploy"
  pigeon search -q "bug" --platform=slack --account=acme-corp
  pigeon search -q "lunch" --since=7d`,
	PreRunE: ensureDaemon,
	RunE: func(cmd *cobra.Command, args []string) error {
		return commands.RunSearch(commands.SearchParams{
			Query:    mustString(cmd, "query"),
			Platform: mustString(cmd, "platform"),
			Account:  mustString(cmd, "account"),
			Since:    mustString(cmd, "since"),
		})
	},
}

func init() {
	searchCmd.Flags().StringP("query", "q", "", "search query")
	searchCmd.Flags().StringP("platform", "p", "", "filter by platform")
	searchCmd.Flags().StringP("account", "a", "", "filter by account")
	searchCmd.Flags().String("since", "", "only search messages from last duration (e.g. 2h, 7d)")
	searchCmd.MarkFlagRequired("query")
	rootCmd.AddCommand(searchCmd)
}
