package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/anish749/pigeon/internal/commands"
	"github.com/anish749/pigeon/internal/paths"
)

func newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List sources visible in the active context",
		GroupID: groupReading,
		Example: `  pigeon list
  pigeon list --context=work
  pigeon list --source=slack
  pigeon list --source=drive --context=work
  pigeon list --since=7d`,
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
			if since != "" {
				return commands.RunListSince(source, account, contextName, since)
			}
			return commands.RunList(source, account, contextName)
		},
	}
	cmd.Flags().String("source", "", "filter by source (gmail, calendar, drive, slack, whatsapp)")
	cmd.Flags().StringP("account", "a", "", "narrow to a specific account")
	cmd.Flags().String("context", "", "context name overriding PIGEON_CONTEXT and default_context")
	cmd.Flags().String("since", "", "only show sources with recent activity (e.g. 2h, 7d)")
	return cmd
}

// activeConv and extractConversations are kept for the existing list-path
// tests, which validate how messaging paths collapse into conversation units.
type activeConv struct {
	Display    string
	Dir        string
	LatestDate string
}

func extractConversations(files []string, root string) []activeConv {
	seen := make(map[string]*activeConv)
	var order []string
	for _, f := range files {
		rel, err := filepath.Rel(root, f)
		if err != nil {
			continue
		}
		parts := strings.Split(rel, string(filepath.Separator))
		isThread := paths.IsThreadFile(f)
		if isThread {
			for i, p := range parts {
				if p == paths.ThreadsSubdir {
					parts = append(parts[:i], parts[i+1:]...)
					break
				}
			}
		}
		if len(parts) < 4 {
			continue
		}
		convDir := filepath.Join(root, parts[0], parts[1], parts[2])

		c, ok := seen[convDir]
		if !ok {
			c = &activeConv{
				Display: strings.Join(parts[:3], "/"),
				Dir:     convDir,
			}
			seen[convDir] = c
			order = append(order, convDir)
		}
		if !isThread {
			dateStr := strings.TrimSuffix(parts[3], paths.FileExt)
			if dateStr > c.LatestDate {
				c.LatestDate = dateStr
			}
		}
	}

	result := make([]activeConv, len(order))
	for i, key := range order {
		result[i] = *seen[key]
	}
	return result
}
