package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/commands"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/read"
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
			accountName, err := cmd.Flags().GetString("account")
			if err != nil {
				return fmt.Errorf("get account flag: %w", err)
			}
			since, err := cmd.Flags().GetString("since")
			if err != nil {
				return fmt.Errorf("get since flag: %w", err)
			}

			if since != "" {
				return runListSince(cmd, platform, accountName, since)
			}

			ws, err := currentWorkspace(cmd)
			if err != nil {
				return err
			}
			if ws != nil {
				// Explicit account must be validated against the workspace.
				if accountName != "" {
					if err := validateAccountInWorkspace(cmd, account.New(platform, accountName)); err != nil {
						return err
					}
					return commands.RunList(platform, accountName)
				}
				return commands.RunListScoped(ws.Accounts, platform)
			}
			return commands.RunList(platform, accountName)
		},
	}
	cmd.Flags().StringP("platform", "p", "", "filter by platform (e.g. whatsapp, slack)")
	cmd.Flags().StringP("account", "a", "", "filter by account (e.g. +14155551234, acme-corp)")
	cmd.Flags().String("since", "", "only show conversations with recent activity (e.g. 2h, 7d)")
	return cmd
}

// runListSince uses read.Glob to find active files, then extracts unique
// conversations and prints them with directory paths.
func runListSince(cmd *cobra.Command, platform, accountName, since string) error {
	sinceDur, err := timeutil.ParseDuration(since)
	if err != nil {
		return fmt.Errorf("invalid --since value %q: %w", since, err)
	}

	dirs, err := resolveSearchDirs(cmd, platform, accountName)
	if err != nil {
		return err
	}

	var allFiles []string
	for _, dir := range dirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue
		}
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
	convs := extractConversations(allFiles, root)
	if len(convs) == 0 {
		fmt.Println("No conversations found.")
		return nil
	}

	now := time.Now()
	for _, c := range convs {
		switch {
		case c.LatestDate != "":
			t, _ := time.Parse("2006-01-02", c.LatestDate)
			fmt.Printf("%s  last: %s ago\n", c.Display, timeutil.FormatAge(now.Sub(t)))
		default:
			fmt.Printf("%s  active\n", c.Display)
		}
		fmt.Printf("  %s\n", c.Dir)
	}
	return nil
}

// activeConv represents a conversation discovered from file paths.
type activeConv struct {
	Display    string // platform/account/conversation
	Dir        string // absolute conversation directory
	LatestDate string // most recent YYYY-MM-DD from date filenames, empty for thread-only
}

// extractConversations deduplicates file paths into unique conversations,
// tracking the most recent date file per conversation for age display.
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
			// Thread files live at <conv>/threads/<ts>.jsonl. Strip the
			// "threads" component so the conversation dir is parts[2].
			// Only strip when the file is actually a thread file — a
			// conversation literally named "threads" has its own
			// YYYY-MM-DD.jsonl children which must not lose the
			// component.
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
