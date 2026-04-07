package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/anish749/pigeon/internal/commands"
	"github.com/anish749/pigeon/internal/read"
)

func newGlobCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "glob",
		Short:   "Find data files by pattern and time window",
		GroupID: groupReading,
		Long: `Finds JSONL data files in the pigeon data directory using ripgrep.

Returns absolute file paths sorted by modification time (most recent first).
Use --since to filter to files within a time window:

  Date files (YYYY-MM-DD.jsonl) are filtered by filename.
  Thread files (threads/*.jsonl) are filtered by content — only threads
  containing messages within the window are returned.

Without --since, all data files are returned.

This is the file discovery tool — use "pigeon grep" for content search.
Output is one file path per line, suitable for piping to other tools.`,
		Example: `  pigeon glob
  pigeon glob --since=2h
  pigeon glob --since=7d --platform=slack
  pigeon glob --platform=slack --account=acme-corp
  pigeon glob --since=24h | xargs jq -r 'select(.type == "msg") | .sender'`,
		PreRunE: ensureDaemon,
		RunE: func(cmd *cobra.Command, args []string) error {
			platform, _ := cmd.Flags().GetString("platform")
			account, _ := cmd.Flags().GetString("account")
			since, _ := cmd.Flags().GetString("since")

			dir := commands.SearchPath(platform, account)
			if _, err := os.Stat(dir); os.IsNotExist(err) {
				return fmt.Errorf("no data at %s", dir)
			}

			var sinceDur time.Duration
			if since != "" {
				d, err := commands.ParseDuration(since)
				if err != nil {
					return fmt.Errorf("invalid --since value %q: %w", since, err)
				}
				sinceDur = d
			}

			files, err := read.Glob(dir, sinceDur)
			if err != nil {
				return err
			}
			for _, f := range files {
				fmt.Println(f)
			}
			return nil
		},
	}
	cmd.Flags().StringP("platform", "p", "", "filter by platform")
	cmd.Flags().StringP("account", "a", "", "filter by account")
	cmd.Flags().String("since", "", "only files with data from last duration (e.g. 2h, 7d)")
	return cmd
}
