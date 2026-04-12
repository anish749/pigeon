package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/anish749/pigeon/internal/commands"
	"github.com/anish749/pigeon/internal/read"
	"github.com/anish749/pigeon/internal/timeutil"
)

func newGlobCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "glob",
		Short:   "Return file paths visible in the active context",
		GroupID: groupReading,
		Long: `Finds data files in the resolved source/account scope.

When --source is omitted, all sources visible in the active context are included.
Without --since, all matching files are returned. With --since, date-based files
are filtered by filename and thread files are filtered by message timestamps.`,
		Example: `  pigeon glob
  pigeon glob --context=work
  pigeon glob --source=gmail --since=7d
  pigeon glob --source=slack -a acme-corp --since=24h`,
		RunE: func(cmd *cobra.Command, args []string) error {
			source, err := cmd.Flags().GetString("source")
			if err != nil {
				return fmt.Errorf("get source flag: %w", err)
			}
			account, err := cmd.Flags().GetString("account")
			if err != nil {
				return fmt.Errorf("get account flag: %w", err)
			}
			contextName, err := cmd.Flags().GetString("context")
			if err != nil {
				return fmt.Errorf("get context flag: %w", err)
			}
			since, err := cmd.Flags().GetString("since")
			if err != nil {
				return fmt.Errorf("get since flag: %w", err)
			}

			scopes, err := commands.ResolveScopes(source, contextName, account)
			if err != nil {
				return err
			}

			var sinceDur time.Duration
			if since != "" {
				d, err := timeutil.ParseDuration(since)
				if err != nil {
					return fmt.Errorf("invalid --since value %q: %w", since, err)
				}
				sinceDur = d
			}

			files, err := read.GlobMany(commandsScopeRoots(scopes), sinceDur)
			if err != nil {
				return err
			}
			for _, file := range files {
				fmt.Println(file)
			}
			return nil
		},
	}
	cmd.Flags().String("source", "", "filter by source")
	cmd.Flags().StringP("account", "a", "", "narrow to a specific account")
	cmd.Flags().String("context", "", "context name overriding PIGEON_CONTEXT and default_context")
	cmd.Flags().String("since", "", "only files with data from last duration (e.g. 2h, 7d)")
	return cmd
}

func commandsScopeRoots(scopes []commands.ResolvedScope) []string {
	var roots []string
	for _, scope := range scopes {
		for _, acct := range scope.Accounts {
			roots = append(roots, commandsSourceRoots(acct, scope.Source)...)
		}
	}
	return roots
}

func commandsSourceRoots(acct commands.ResolvedAccount, source commands.Source) []string {
	return commands.SourceRootsForCLI(acct, source)
}
