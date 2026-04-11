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
			platform, _ := cmd.Flags().GetString("platform")
			account, _ := cmd.Flags().GetString("account")
			since, _ := cmd.Flags().GetString("since")
			contextFlag, _ := cmd.Flags().GetString("context")

			// Context-aware listing.
			if contextFlag != "" || hasActiveContext() {
				return runListContext(contextFlag, since)
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

// hasActiveContext checks if a context is active via env or config default.
func hasActiveContext() bool {
	if os.Getenv("PIGEON_CONTEXT") != "" {
		return true
	}
	cfg, err := config.Load()
	if err != nil {
		return false
	}
	return cfg.DefaultContext != ""
}

// runListContext lists sources for the active context.
func runListContext(contextFlag, _ string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	ctxName := contextFlag
	if ctxName == "" {
		ctxName = os.Getenv("PIGEON_CONTEXT")
	}
	if ctxName == "" {
		ctxName = cfg.DefaultContext
	}
	if ctxName == "" {
		return fmt.Errorf("no context active — use --context or set default_context in config")
	}

	ctx, ok := cfg.Contexts[ctxName]
	if !ok {
		return fmt.Errorf("unknown context %q", ctxName)
	}

	fmt.Printf("Sources for %s:\n\n", ctxName)

	// List each platform's sources in the context.
	for _, src := range pctx.AllSources {
		platform := src.ContextKey()
		identifiers := ctx.Accounts(platform)
		if len(identifiers) == 0 {
			continue
		}
		for _, id := range identifiers {
			fmt.Printf("  %-36s %s\n", src, id)
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
