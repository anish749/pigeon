package commands

import (
	"flag"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/anish/claude-msg-utils/internal/store"
)

func RunRead(args []string) error {
	fs := flag.NewFlagSet("read", flag.ExitOnError)
	platform := fs.String("platform", "", "platform (e.g. whatsapp, slack) [required]")
	account := fs.String("account", "", "account (e.g. +14155551234, acme-corp) [required]")
	contact := fs.String("contact", "", "contact name, phone, or channel to search for [required]")
	date := fs.String("date", "", "specific date (YYYY-MM-DD)")
	last := fs.Int("last", 0, "show last N messages")
	since := fs.String("since", "", "show messages from last duration (e.g. 2h, 30m, 7d)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *platform == "" || *account == "" || *contact == "" {
		return fmt.Errorf("required flags: -platform, -account, -contact")
	}

	aliases := loadAliases(*platform, *account)
	conv, err := store.FindConversation(*platform, *account, *contact, aliases)
	if err != nil {
		return err
	}

	opts := store.ReadOpts{
		Date: *date,
		Last: *last,
	}
	if *since != "" {
		d, err := parseDuration(*since)
		if err != nil {
			return fmt.Errorf("invalid -since value %q: %w", *since, err)
		}
		opts.Since = d
	}

	lines, err := store.ReadMessages(*platform, *account, conv.DirName, opts)
	if err != nil {
		return err
	}
	if len(lines) == 0 {
		fmt.Println("No messages found.")
		return nil
	}

	dir := filepath.Join(store.DataDir(), *platform, *account, conv.DirName)
	fmt.Printf("--- %s/%s/%s ---\n", *platform, *account, conv.DisplayName)
	fmt.Printf("    %s\n", dir)
	fmt.Println(strings.Join(lines, "\n"))
	return nil
}

// parseDuration handles Go durations plus "d" for days.
func parseDuration(s string) (time.Duration, error) {
	if rest, ok := strings.CutSuffix(s, "d"); ok {
		var days int
		if _, err := fmt.Sscanf(rest, "%d", &days); err != nil {
			return 0, err
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}
