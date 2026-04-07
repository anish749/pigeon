package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/anish749/pigeon/internal/commands"
	"github.com/anish749/pigeon/internal/read"
	"github.com/anish749/pigeon/internal/search"
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

Output is raw rg format: filepath:matching_line. Pipe to jq for
structured queries on the JSON content.

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
  pigeon grep -q "deploy" | jq -r 'select(.type == "msg") | .sender + ": " + .text'`,
		PreRunE: ensureDaemon,
		RunE: func(cmd *cobra.Command, args []string) error {
			query, _ := cmd.Flags().GetString("query")
			platform, _ := cmd.Flags().GetString("platform")
			account, _ := cmd.Flags().GetString("account")
			since, _ := cmd.Flags().GetString("since")
			context, _ := cmd.Flags().GetInt("context")
			filesOnly, _ := cmd.Flags().GetBool("files-with-matches")
			count, _ := cmd.Flags().GetBool("count")
			caseInsensitive, _ := cmd.Flags().GetBool("ignore-case")
			fixedStrings, _ := cmd.Flags().GetBool("fixed-strings")

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

			out, err := read.Grep(dir, read.GrepOpts{
				Query:           query,
				Since:           sinceDur,
				Context:         context,
				FilesOnly:       filesOnly,
				Count:           count,
				CaseInsensitive: caseInsensitive,
				FixedStrings:    fixedStrings,
			})
			if err != nil {
				return err
			}

			if len(out) == 0 {
				fmt.Println("No matches found.")
				return nil
			}

			// Raw rg output — when piped to jq, the caller gets
			// structured JSONL. When -l or -c, output is already
			// file paths or counts.
			if filesOnly || count {
				os.Stdout.Write(out)
				return nil
			}

			// For content output, also print the parsed summary if
			// stdout is a terminal (not piped).
			if isTerminal() {
				matches, parseErr := search.ParseGrepOutput(out, dir)
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
	cmd.Flags().StringP("query", "q", "", "ripgrep search pattern (regex by default, use -F for literal)")
	cmd.Flags().StringP("platform", "p", "", "filter by platform")
	cmd.Flags().StringP("account", "a", "", "filter by account")
	cmd.Flags().String("since", "", "only search messages from last duration (e.g. 2h, 7d)")
	cmd.Flags().IntP("context", "C", 7, "lines of context around each match")
	cmd.Flags().BoolP("files-with-matches", "l", false, "print only file paths containing matches")
	cmd.Flags().BoolP("count", "c", false, "print match count per file")
	cmd.Flags().BoolP("ignore-case", "i", false, "case insensitive search")
	cmd.Flags().BoolP("fixed-strings", "F", false, "treat query as literal string, not regex")
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
