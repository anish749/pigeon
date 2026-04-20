package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/anish749/pigeon/internal/read"
	"github.com/anish749/pigeon/internal/search"
	"github.com/anish749/pigeon/internal/timeutil"
)

func newGrepCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "grep",
		Aliases: []string{"rg", "search"},
		Short:   "Search message content with ripgrep",
		GroupID: groupReading,
		Long: `Searches JSONL message files using ripgrep (rg).

The query is a ripgrep pattern — full regex syntax is supported.
Use -F for literal string matching (no regex interpretation).

Platform and account flags narrow the search to a subdirectory.
The --since flag restricts date files by filename and includes
thread files containing messages within the window.

Flags -l, -c, -i, -F, and -C are passed through to rg. See
rg --help for full documentation of pattern syntax and behavior.

Output is raw rg format: filepath:matching_line. To pipe to jq,
use --no-filename -C 0 so each output line is valid JSON.

JSON fields in each line:
  type      event type: "msg", "react", "unreact", "edit", "delete", "separator"
  ts        timestamp (ISO 8601, e.g. "2026-03-16T09:15:02Z")
  id        message ID (on msg events)
  msg       target message ID (on react/edit/delete events)
  sender    display name
  from      platform user ID (stable identity)
  text      message body (on msg/edit events)
  via       message pathway: "to-pigeon", "pigeon-as-user", "pigeon-as-bot"
  emoji     reaction emoji (on react/unreact events)
  attach    attachments array, each with "id" and "type" (MIME)
  reply     true if thread reply (on msg events)
  replyTo   quoted message ID (on msg events, WhatsApp quote-reply)`,
		Example: `  pigeon grep -q "deploy"
  pigeon grep -q "deploy" --since=7d
  pigeon grep -q "deploy" -l                        # file paths only
  pigeon grep -q "deploy" -c                        # match counts per file
  pigeon grep -q "deploy" -i                        # case insensitive
  pigeon grep -q "Alice" -F                         # literal match, no regex
  pigeon grep -q "bug" -p slack -a acme-corp -C 3
  pigeon grep -q "deploy" --no-filename -C 0 | jq 'select(.type == "msg")'
  pigeon grep -q "Alice" --no-filename -C 0 | jq -r '"[" + .ts[11:19] + "] " + .sender + ": " + .text'`,
		PreRunE: ensureDaemon,
		RunE: func(cmd *cobra.Command, args []string) error {
			query, err := cmd.Flags().GetString("query")
			if err != nil {
				return fmt.Errorf("get query flag: %w", err)
			}
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
			context, err := cmd.Flags().GetInt("context")
			if err != nil {
				return fmt.Errorf("get context flag: %w", err)
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
			noFilename, err := cmd.Flags().GetBool("no-filename")
			if err != nil {
				return fmt.Errorf("get no-filename flag: %w", err)
			}

			ws, err := currentWorkspace(cmd)
			if err != nil {
				return err
			}
			dirs, err := read.SearchDirs(ws, platform, account)
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

			// Terminal: use rg --json for structured parsing.
			// Pipe: use raw rg output for jq compatibility.
			if isTerminal() && !filesOnly && !count {
				var allMatches []search.Match
				for _, dir := range dirs {
					if _, err := os.Stat(dir); os.IsNotExist(err) {
						continue
					}
					out, err := read.Grep(dir, read.GrepOpts{
						Query:           query,
						Since:           sinceDur,
						Context:         context,
						CaseInsensitive: caseInsensitive,
						FixedStrings:    fixedStrings,
						JSON:            true,
					})
					if err != nil {
						return err
					}
					matches, parseErr := search.ParseGrepOutput(out, dir)
					if parseErr != nil {
						fmt.Fprintf(os.Stderr, "warning: some lines failed to parse: %v\n", parseErr)
					}
					allMatches = append(allMatches, matches...)
				}
				if sinceDur > 0 {
					allMatches = search.FilterThreadsBySince(allMatches, sinceDur)
				}
				if len(allMatches) == 0 {
					fmt.Println("No matches found.")
					return nil
				}
				fmt.Printf("%d match(es) found:\n\n", len(allMatches))
				search.PrintSummary(allMatches, sinceDur)
				search.PrintGroupedResults(allMatches)
				return nil
			}

			var allOut []byte
			for _, dir := range dirs {
				if _, err := os.Stat(dir); os.IsNotExist(err) {
					continue
				}
				out, err := read.Grep(dir, read.GrepOpts{
					Query:           query,
					Since:           sinceDur,
					Context:         context,
					FilesOnly:       filesOnly,
					Count:           count,
					CaseInsensitive: caseInsensitive,
					FixedStrings:    fixedStrings,
					NoFilename:      noFilename,
				})
				if err != nil {
					return err
				}
				allOut = append(allOut, out...)
			}
			if len(allOut) == 0 {
				fmt.Println("No matches found.")
				return nil
			}
			os.Stdout.Write(allOut)
			return nil
		},
	}
	cmd.Flags().StringP("query", "q", "", "ripgrep search pattern (regex by default, use -F for literal)")
	cmd.Flags().StringP("platform", "p", "", "filter by platform")
	cmd.Flags().StringP("account", "a", "", "filter by account")
	cmd.Flags().String("since", "", "only search messages from last duration (e.g. 2h, 7d)")
	cmd.Flags().IntP("context", "C", 7, "lines of context around each match")
	cmd.Flags().BoolP("files-with-matches", "l", false, "print only file paths containing matches")
	cmd.Flags().BoolP("count", "c", false, "print match count per file")
	cmd.Flags().BoolP("ignore-case", "i", false, "case insensitive search")
	cmd.Flags().BoolP("fixed-strings", "F", false, "treat query as literal string, not regex")
	cmd.Flags().Bool("no-filename", false, "omit file paths from output")
	cmd.MarkFlagRequired("query")
	return cmd
}

func isTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
