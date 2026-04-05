package cli

import (
	"fmt"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/anish749/pigeon/internal/commands"
)

func newSearchCmd() *cobra.Command {
	hasRg := exec.Command("rg", "--version").Run() == nil

	name := "grep"
	short := "Search messages with grep (install ripgrep for better results)"
	if hasRg {
		name = "rg"
		short = "Search messages with ripgrep"
	}

	long := fmt.Sprintf(`Searches JSONL message files using %s under the hood.

Platform and account flags narrow the search to a subdirectory.
The --since flag restricts to date files within the time window.
Flags -C and -q are passed through to %s.

Output is raw JSONL — one JSON object per line. Pipe through jq
for structured queries.

JSON fields:
  type      event type: "msg", "react", "unreact", "edit", "delete"
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
  replyTo   quoted message ID (on msg events, WhatsApp quote-reply)

jq examples:
  pigeon %s -q "deploy" | jq 'select(.type == "msg")'
  pigeon %s -q "Alice" | jq -r '"[" + .ts[11:19] + "] " + .sender + ": " + .text'
  pigeon %s -q "bug" | jq 'select(.sender == "Bob" and .attach != null)'`, name, name, name, name, name)

	cmd := &cobra.Command{
		Use:     name,
		Aliases: []string{"search"},
		Short:   short,
		Long:    long,
		GroupID: groupReading,
		Example: fmt.Sprintf(`  pigeon %s -q "deploy"
  pigeon %s -q "bug" --platform=slack --account=acme-corp
  pigeon %s -q "lunch" --since=7d
  pigeon %s -q "deploy" -C 3
  pigeon %s -q "deploy" | jq -r 'select(.type == "msg") | .sender + ": " + .text'`, name, name, name, name, name),
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
			context, err := cmd.Flags().GetInt("context")
			if err != nil {
				return err
			}
			return commands.RunSearch(commands.SearchParams{
				Query:    query,
				Platform: platform,
				Account:  account,
				Since:    since,
				Context:  context,
			})
		},
	}
	cmd.Flags().StringP("query", "q", "", "search query")
	cmd.Flags().StringP("platform", "p", "", "filter by platform")
	cmd.Flags().StringP("account", "a", "", "filter by account")
	cmd.Flags().String("since", "", "only search messages from last duration (e.g. 2h, 7d)")
	cmd.Flags().IntP("context", "C", 7, "lines of context around each match")
	cmd.MarkFlagRequired("query")
	return cmd
}
