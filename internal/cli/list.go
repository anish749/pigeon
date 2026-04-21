package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	a "github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/commands"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/read"
	"github.com/anish749/pigeon/internal/store/filekinds"
	"github.com/anish749/pigeon/internal/timeutil"
)

func newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List platforms, accounts, or conversations",
		GroupID: groupReading,
		Example: `  pigeon list
  pigeon list --platform=slack
  pigeon list --platform=whatsapp --account=+14155551234
  pigeon list --since=2h
  pigeon list --since=7d --platform=slack`,
		PreRunE: ensureDaemon,
		RunE: func(cmd *cobra.Command, args []string) error {
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
			if since != "" {
				return runListSince(platform, account, since)
			}

			if activeWorkspace != nil {
				if account != "" {
					if err := validateAccountInWorkspace(a.New(platform, account)); err != nil {
						return err
					}
					return commands.RunList(platform, account)
				}
				return commands.RunListScoped(activeWorkspace.Accounts, platform)
			}
			return commands.RunList(platform, account)
		},
	}
	cmd.Flags().StringP("platform", "p", "", "filter by platform (e.g. whatsapp, slack)")
	cmd.Flags().StringP("account", "a", "", "filter by account (e.g. +14155551234, acme-corp)")
	cmd.Flags().String("since", "", "only show conversations with recent activity (e.g. 2h, 7d)")
	return cmd
}

// runListSince uses read.Glob to find active files, then extracts unique
// conversations and prints them with directory paths.
func runListSince(platform, account, since string) error {
	sinceDur, err := timeutil.ParseDuration(since)
	if err != nil {
		return fmt.Errorf("invalid --since value %q: %w", since, err)
	}

	dirs, err := read.SearchDirs(activeWorkspace, platform, account)
	if err != nil {
		return err
	}

	var allFiles []string
	for _, dir := range dirs {
		files, err := read.Glob(dir, sinceDur)
		if err != nil {
			return err
		}
		allFiles = append(allFiles, files...)
	}
	if len(allFiles) == 0 {
		fmt.Println("No conversations found.")
		return nil
	}

	root := paths.DefaultDataRoot().Path()
	convs, err := extractConversations(allFiles, root)
	if err != nil {
		return err
	}
	if len(convs) == 0 {
		fmt.Println("No conversations found.")
		return nil
	}

	now := time.Now()
	for _, c := range convs {
		if !c.LatestTime.IsZero() {
			fmt.Printf("%s  last: %s ago\n", c.Display, timeutil.FormatAge(now.Sub(c.LatestTime)))
		} else {
			fmt.Printf("%s  active\n", c.Display)
		}
		fmt.Printf("  %s\n", c.Dir)
	}
	return nil
}

// activeConv represents a conversation discovered from file paths.
type activeConv struct {
	Display    string    // platform/account/conversation
	Dir        string    // absolute conversation directory
	LatestTime time.Time // most recent message timestamp from file content
}

// extractConversations deduplicates file paths into unique conversations,
// tracking the most recent message timestamp per conversation. The per-file
// conversation identity (grouping key + display label) is delegated to the
// matching filekinds.Kind, so every source — slack channels, gmail inboxes,
// individual calendars, individual Drive docs, individual Linear issues —
// groups at its own natural granularity rather than a fixed path depth.
func extractConversations(files []string, root string) ([]activeConv, error) {
	seen := make(map[string]*activeConv)
	var order []string
	for _, f := range files {
		kind := filekinds.For(f)
		if kind == nil {
			continue
		}
		conv := kind.Conversation(f, root)

		c, ok := seen[conv.Dir]
		if !ok {
			c = &activeConv{Display: conv.Display, Dir: conv.Dir}
			seen[conv.Dir] = c
			order = append(order, conv.Dir)
		}

		ts, err := kind.LatestTs(f)
		if err != nil {
			return nil, fmt.Errorf("latest ts %s: %w", f, err)
		}
		if ts.After(c.LatestTime) {
			c.LatestTime = ts
		}
	}

	result := make([]activeConv, len(order))
	for i, key := range order {
		result[i] = *seen[key]
	}
	return result, nil
}
