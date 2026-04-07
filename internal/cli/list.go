package cli

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

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

	dir := paths.SearchDir(platform, account)
	files, err := read.Glob(dir, sinceDur)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		fmt.Println("No conversations found.")
		return nil
	}

	// Extract unique conversations from file paths, tracking the most
	// recent date file per conversation for the "last" label.
	root := paths.DefaultDataRoot().Path()
	type convInfo struct {
		display    string // platform/account/conversation
		dir        string // absolute conversation directory
		latestDate string // most recent YYYY-MM-DD from filenames
	}
	seen := make(map[string]*convInfo) // dir → info
	var order []string
	for _, f := range files {
		rel, err := filepath.Rel(root, f)
		if err != nil {
			continue
		}
		parts := strings.Split(rel, string(filepath.Separator))
		// Strip "threads" dir if present.
		for i, p := range parts {
			if p == paths.ThreadsSubdir {
				parts = append(parts[:i], parts[i+1:]...)
				break
			}
		}
		// Need at least platform/account/conversation/file.
		if len(parts) < 4 {
			continue
		}
		convDir := filepath.Join(root, parts[0], parts[1], parts[2])
		dateStr := strings.TrimSuffix(parts[3], paths.FileExt)

		info, ok := seen[convDir]
		if !ok {
			info = &convInfo{
				display: strings.Join(parts[:3], "/"),
				dir:     convDir,
			}
			seen[convDir] = info
			order = append(order, convDir)
		}
		// Track the most recent date filename (lexically largest = most recent).
		if dateStr > info.latestDate {
			info.latestDate = dateStr
		}
	}

	now := time.Now()
	for _, key := range order {
		info := seen[key]
		if t, err := time.Parse("2006-01-02", info.latestDate); err == nil {
			fmt.Printf("%s  last: %s ago\n", info.display, timeutil.FormatAge(now.Sub(t)))
		} else {
			fmt.Println(info.display)
		}
		fmt.Printf("  %s\n", info.dir)
	}
	return nil
}
