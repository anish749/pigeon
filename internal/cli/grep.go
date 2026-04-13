package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/anish749/pigeon/internal/commands"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/read"
	"github.com/anish749/pigeon/internal/search"
	"github.com/anish749/pigeon/internal/timeutil"
)

func newGrepCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "grep <query>",
		Aliases: []string{"rg", "search"},
		Short:   "Search content within the active context",
		GroupID: groupReading,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("usage: pigeon grep <query>")
			}
			return nil
		},
		Example: `  pigeon grep deploy
  pigeon grep deploy --since=7d
  pigeon grep deploy --source=slack
  pigeon grep quarterly --source=drive -l
  pigeon grep deploy --context=work -C 3`,
		PreRunE: ensureDaemon,
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
			contextLines, err := cmd.Flags().GetInt("context-lines")
			if err != nil {
				return fmt.Errorf("get context-lines flag: %w", err)
			}
			filesOnly, err := cmd.Flags().GetBool("files-with-matches")
			if err != nil {
				return fmt.Errorf("get files-with-matches flag: %w", err)
			}
			count, err := cmd.Flags().GetBool("count")
			if err != nil {
				return fmt.Errorf("get count flag: %w", err)
			}
			caseInsensitive, err := cmd.Flags().GetBool("ignore-case")
			if err != nil {
				return fmt.Errorf("get ignore-case flag: %w", err)
			}
			fixedStrings, err := cmd.Flags().GetBool("fixed-strings")
			if err != nil {
				return fmt.Errorf("get fixed-strings flag: %w", err)
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

			roots := commandsScopeRoots(scopes)
			out, err := read.GrepMany(roots, read.GrepOpts{
				Query:           args[0],
				Since:           sinceDur,
				Context:         contextLines,
				FilesOnly:       filesOnly,
				Count:           count,
				CaseInsensitive: caseInsensitive,
				FixedStrings:    fixedStrings,
				JSON:            isTerminal() && !filesOnly && !count,
			})
			if err != nil {
				return err
			}
			if len(out) == 0 {
				fmt.Println("No matches found.")
				return nil
			}

			if isTerminal() && !filesOnly && !count {
				matches, parseErr := search.ParseGrepOutput(out, paths.DefaultDataRoot().Path())
				if parseErr != nil {
					fmt.Fprintf(os.Stderr, "warning: some lines failed to parse: %v\n", parseErr)
				}
				if sinceDur > 0 {
					matches = search.FilterThreadsBySince(matches, sinceDur)
				}
				if len(matches) == 0 {
					fmt.Println("No matches found.")
					return nil
				}
				fmt.Printf("%d match(es) found:\n\n", len(matches))
				search.PrintSummary(matches, sinceDur)
				search.PrintGroupedResults(matches)
				return nil
			}

			os.Stdout.Write(out)
			return nil
		},
	}
	cmd.Flags().String("source", "", "filter by source")
	cmd.Flags().StringP("account", "a", "", "narrow to a specific account")
	cmd.Flags().String("context", "", "context name overriding PIGEON_CONTEXT and default_context")
	cmd.Flags().String("since", "", "only search items from last duration (e.g. 2h, 7d)")
	cmd.Flags().IntP("context-lines", "C", 7, "lines of context around each match")
	cmd.Flags().BoolP("files-with-matches", "l", false, "print only file paths containing matches")
	cmd.Flags().BoolP("count", "c", false, "print match count per file")
	cmd.Flags().BoolP("ignore-case", "i", false, "case insensitive search")
	cmd.Flags().BoolP("fixed-strings", "F", false, "treat query as literal string, not regex")
	return cmd
}

func isTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
