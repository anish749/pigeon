package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/pctx"
	"github.com/anish749/pigeon/internal/reader"
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
	// Parse source.
	src, err := pctx.ParseSource(args[0])
	if err != nil {
		return err
	}

	// Optional selector.
	selector := ""
	if len(args) > 1 {
		selector = args[1]
	}

	// Load config and resolve context.
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

	resolved, err := pctx.Resolve(cfg, src, pctx.ResolveOpts{
		Context: contextFlag,
		Account: accountFlag,
	})
	if err != nil {
		return err
	}

	// Parse filters.
	filters, err := parseFilters(cmd)
	if err != nil {
		return err
	}

	// Dispatch to source-specific reader.
	root := paths.DefaultDataRoot()
	acct := resolved.Accounts[0] // use first resolved account

	switch src {
	case pctx.SourceGmail:
		gmailDir := root.AccountFor(acct).Gmail()
		result, err := reader.ReadGmail(gmailDir, filters)
		if err != nil {
			return err
		}
		fmt.Print(reader.FormatGmail(result))
		return nil

	case pctx.SourceCalendar:
		calID, err := cmd.Flags().GetString("calendar")
		if err != nil {
			return fmt.Errorf("get calendar flag: %w", err)
		}
		if calID == "" {
			calID = "primary"
		}
		calDir := root.AccountFor(acct).Calendar(calID)
		result, err := reader.ReadCalendar(calDir, filters)
		if err != nil {
			return err
		}
		fmt.Print(reader.FormatCalendar(result))
		return nil

	case pctx.SourceDrive:
		if selector == "" {
			return fmt.Errorf("pigeon read drive requires a document name")
		}
		driveDir := root.AccountFor(acct).Drive()
		fileDir, err := reader.FindDriveFile(driveDir, selector)
		if err != nil {
			return err
		}
		result, err := reader.ReadDrive(fileDir)
		if err != nil {
			return err
		}
		fmt.Print(reader.FormatDrive(result))
		return nil

	case pctx.SourceSlack:
		if selector == "" {
			return fmt.Errorf("pigeon read slack requires a channel or DM (e.g. #engineering, @alice)")
		}
		s := store.NewFSStore(root)
		result, err := reader.ReadMessaging(s, acct, selector, filters)
		if err != nil {
			return err
		}
		return printMessagingResult(result)

	case pctx.SourceWhatsApp:
		if selector == "" {
			return fmt.Errorf("pigeon read whatsapp requires a contact or group name")
		}
		s := store.NewFSStore(root)
		result, err := reader.ReadMessaging(s, acct, selector, filters)
		if err != nil {
			return err
		}
		return printMessagingResult(result)

	case pctx.SourceLinear:
		accountDir := root.AccountFor(acct)
		if selector != "" {
			// Try exact identifier first, then fuzzy.
			identifier, err := reader.FindLinearIssue(accountDir, selector)
			if err != nil {
				return err
			}
			result, err := reader.ReadLinearIssue(accountDir, identifier)
			if err != nil {
				return err
			}
			fmt.Print(reader.FormatLinearIssue(result))
		} else {
			result, err := reader.ListLinearIssues(accountDir, filters)
			if err != nil {
				return err
			}
			fmt.Print(reader.FormatLinearList(result))
		}
		return nil

	default:
		return fmt.Errorf("source %q not yet implemented", src)
	}
}

func parseFilters(cmd *cobra.Command) (reader.Filters, error) {
	var filters reader.Filters

	date, err := cmd.Flags().GetString("date")
	if err != nil {
		return filters, fmt.Errorf("get date flag: %w", err)
	}
	filters.Date = date

	last, err := cmd.Flags().GetInt("last")
	if err != nil {
		return filters, fmt.Errorf("get last flag: %w", err)
	}
	filters.Last = last

	since, err := cmd.Flags().GetString("since")
	if err != nil {
		return filters, fmt.Errorf("get since flag: %w", err)
	}
	if since != "" {
		d, err := timeutil.ParseDuration(since)
		if err != nil {
			return filters, fmt.Errorf("invalid --since value %q: %w", since, err)
		}
		filters.Since = d
	}

	return filters, nil
}

func printMessagingResult(result *reader.MessagingResult) error {
	if result.Messages == nil || len(result.Messages.Messages) == 0 {
		fmt.Println("No messages found.")
		return nil
	}

	lines := modelv1.FormatDateFile(result.Messages, time.Local)
	header := fmt.Sprintf("--- %s/%s ---", result.Account.Display(), result.DisplayName)
	fmt.Println(header)
	fmt.Println(strings.Join(lines, "\n"))
	return nil
}
