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
	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/pctx"
	"github.com/anish749/pigeon/internal/store"
	"github.com/anish749/pigeon/internal/store/modelv1"
	"github.com/anish749/pigeon/internal/timeutil"
)

func newReadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "read <source> [selector]",
		Short: "Read data from a source",
		Long: `Read data from a source within the active context.

Sources: gmail, calendar, drive, slack, whatsapp, linear

The active context determines which accounts are queried. Override with
--context or -a. When no context is set and only one account exists for
the source's platform, it is used automatically.`,
		GroupID: groupReading,
		Example: `  pigeon read gmail --since=7d
  pigeon read calendar
  pigeon read drive "Q2 Planning"
  pigeon read slack #engineering --since=2h
  pigeon read whatsapp Alice --last=20
  pigeon read linear TRU-253
  pigeon read gmail --context=work --since=24h
  pigeon read calendar -a work@company.com`,
		Args:    cobra.RangeArgs(1, 2),
		PreRunE: ensureDaemon,
		RunE:    runRead,
	}
	cmd.Flags().String("context", "", "override active context")
	cmd.Flags().StringP("account", "a", "", "bypass context, use specific account")
	cmd.Flags().String("date", "", "specific date (YYYY-MM-DD)")
	cmd.Flags().Int("last", 0, "last N items")
	cmd.Flags().String("since", "", "items within duration (e.g. 30m, 2h, 7d)")
	cmd.Flags().String("calendar", "", "calendar ID (default: primary)")
	return cmd
}

func runRead(cmd *cobra.Command, args []string) error {
	src, err := pctx.ParseSource(args[0])
	if err != nil {
		return err
	}

	selector := ""
	if len(args) > 1 {
		selector = args[1]
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	contextFlag, err := cmd.Flags().GetString("context")
	if err != nil {
		return fmt.Errorf("get context flag: %w", err)
	}
	accountFlag, err := cmd.Flags().GetString("account")
	if err != nil {
		return fmt.Errorf("get account flag: %w", err)
	}

	ctxName := pctx.ResolveContextName(contextFlag, os.Getenv("PIGEON_CONTEXT"), cfg)
	resolved, err := pctx.Resolve(cfg, src, pctx.ResolveOpts{
		Context: ctxName,
		Account: accountFlag,
	})
	if err != nil {
		return err
	}

	root := paths.DefaultDataRoot()
	s := store.NewFSStore(root)
	acct := resolved.Accounts[0]

	since, date, last, err := parseTimeFilters(cmd)
	if err != nil {
		return err
	}

	switch src {
	case pctx.SourceGmail:
		lines, err := commands.ReadGWSLines(s, root.AccountFor(acct).Gmail().Path(), since, date, last, commands.DefaultGmailLast)
		return printLinesResult(lines, err)

	case pctx.SourceCalendar:
		calID, err := cmd.Flags().GetString("calendar")
		if err != nil {
			return fmt.Errorf("get calendar flag: %w", err)
		}
		if calID == "" {
			calID = "primary"
		}
		// Calendar defaults to today when no filter specified.
		if since == 0 && date == "" && last == 0 {
			date = time.Now().Format("2006-01-02")
		}
		lines, err := commands.ReadGWSLines(s, root.AccountFor(acct).Calendar(calID).Path(), since, date, last, 0)
		return printLinesResult(lines, err)

	case pctx.SourceDrive:
		if selector == "" {
			return fmt.Errorf("pigeon read drive requires a document name")
		}
		content, comments, err := commands.ReadDriveContent(s, root.AccountFor(acct).Drive(), selector)
		if err != nil {
			return err
		}
		if content != "" {
			fmt.Print(content)
		}
		return printLinesResult(comments, nil)

	case pctx.SourceSlack:
		if selector == "" {
			return fmt.Errorf("pigeon read slack requires a channel or DM (e.g. #engineering, @alice)")
		}
		return runReadMessaging(s, acct, selector, since, date, last)

	case pctx.SourceWhatsApp:
		if selector == "" {
			return fmt.Errorf("pigeon read whatsapp requires a contact or group name")
		}
		return runReadMessaging(s, acct, selector, since, date, last)

	case pctx.SourceLinear:
		if selector == "" {
			return fmt.Errorf("pigeon read linear requires an issue identifier")
		}
		issuesDir := filepath.Join(root.AccountFor(acct).Path(), "issues")
		lines, err := commands.ReadLinearIssue(s, issuesDir, selector)
		return printLinesResult(lines, err)

	default:
		return fmt.Errorf("source %q not yet implemented", src)
	}
}

// runReadMessaging handles the messaging read path (slack, whatsapp) with
// the existing store + formatter.
func runReadMessaging(s *store.FSStore, acct account.Account, selector string, since time.Duration, date string, last int) error {
	df, _, err := commands.ReadMessaging(s, acct, selector, since, date, last)
	if err != nil {
		return err
	}
	if df == nil || len(df.Messages) == 0 {
		fmt.Println("No messages found.")
		return nil
	}
	lines := modelv1.FormatDateFile(df, time.Local)
	fmt.Println(strings.Join(lines, "\n"))
	return nil
}

func parseTimeFilters(cmd *cobra.Command) (since time.Duration, date string, last int, err error) {
	dateStr, err := cmd.Flags().GetString("date")
	if err != nil {
		return 0, "", 0, fmt.Errorf("get date flag: %w", err)
	}
	lastN, err := cmd.Flags().GetInt("last")
	if err != nil {
		return 0, "", 0, fmt.Errorf("get last flag: %w", err)
	}
	sinceStr, err := cmd.Flags().GetString("since")
	if err != nil {
		return 0, "", 0, fmt.Errorf("get since flag: %w", err)
	}
	var sinceDur time.Duration
	if sinceStr != "" {
		sinceDur, err = timeutil.ParseDuration(sinceStr)
		if err != nil {
			return 0, "", 0, fmt.Errorf("invalid --since value %q: %w", sinceStr, err)
		}
	}
	return sinceDur, dateStr, lastN, nil
}

// printLinesResult marshals lines to JSON and prints to stdout. Errors are
// printed to stderr so they don't pollute the JSON output.
func printLinesResult(lines []modelv1.Line, readErr error) error {
	for _, l := range lines {
		data, err := modelv1.Marshal(l)
		if err != nil {
			fmt.Fprintf(os.Stderr, "⚠ marshal: %s\n", err)
			continue
		}
		fmt.Println(string(data))
	}
	if readErr != nil {
		fmt.Fprintf(os.Stderr, "⚠ %s\n", readErr)
	}
	return nil
}
