package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/anish749/pigeon/internal/commands"
	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/pctx"
	"github.com/anish749/pigeon/internal/read"
	"github.com/anish749/pigeon/internal/timeutil"
)

func newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List sources and conversations",
		GroupID: groupReading,
		Example: `  pigeon list
  pigeon list --context=work
  pigeon list --since=2h
  pigeon list --platform=slack
  pigeon list --platform=whatsapp --account=+14155551234`,
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
			contextFlag, err := cmd.Flags().GetString("context")
			if err != nil {
				return fmt.Errorf("get context flag: %w", err)
			}

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			ctxName := pctx.ResolveContextName(contextFlag, os.Getenv("PIGEON_CONTEXT"), cfg)

			// Context-aware listing.
			if ctxName != "" {
				return runListContext(cfg, ctxName)
			}

			// Legacy listing (no context).
			if since != "" {
				return runListSince(platform, account, since)
			}
			return commands.RunList(platform, account)
		},
	}
	cmd.Flags().StringP("platform", "p", "", "filter by platform")
	cmd.Flags().StringP("account", "a", "", "filter by account")
	cmd.Flags().String("since", "", "only show sources with recent activity (e.g. 2h, 7d)")
	cmd.Flags().String("context", "", "override active context")
	return cmd
}

// runListContext lists sources for the active context. The context name
// is already resolved by the caller — this function never reads env vars.
func runListContext(cfg *config.Config, ctxName pctx.ContextName) error {
	ctx, ok := cfg.Contexts[string(ctxName)]
	if !ok {
		return fmt.Errorf("unknown context %q", ctxName)
	}

	fmt.Printf("Sources for %s:\n\n", ctxName)

	type sourceEntry struct {
		source string
		ids    []string
	}
	entries := []sourceEntry{
		{"gmail", ctx.GWS},
		{"calendar", ctx.GWS},
		{"drive", ctx.GWS},
		{"slack", ctx.Slack},
		{"whatsapp", ctx.WhatsApp},
	}
	for _, e := range entries {
		for _, id := range e.ids {
			fmt.Printf("  %-36s %s\n", e.source, id)
		}
	}
	return nil
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

	root := paths.DefaultDataRoot().Path()
	convs := extractConversations(files, root)
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
