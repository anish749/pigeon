package commands

import (
	"flag"
	"fmt"
	"path/filepath"
	"time"

	"github.com/anish/claude-msg-utils/internal/store"
)

func RunSearch(args []string) error {
	fs := flag.NewFlagSet("search", flag.ExitOnError)
	query := fs.String("q", "", "search query [required]")
	platform := fs.String("platform", "", "filter by platform")
	account := fs.String("account", "", "filter by account")
	since := fs.String("since", "", "only search messages from last duration (e.g. 2h, 7d)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *query == "" {
		return fmt.Errorf("required flag: -q")
	}

	var sinceDur time.Duration
	if *since != "" {
		d, err := parseDuration(*since)
		if err != nil {
			return fmt.Errorf("invalid -since value %q: %w", *since, err)
		}
		sinceDur = d
	}

	results, err := store.SearchMessages(*query, *platform, *account, sinceDur)
	if err != nil {
		return err
	}
	if len(results) == 0 {
		fmt.Println("No matches found.")
		return nil
	}

	fmt.Printf("%d match(es) found:\n\n", len(results))
	for _, r := range results {
		dir := filepath.Join(store.DataDir(), r.Platform, r.Account, r.Conversation)
		fmt.Printf("[%s/%s/%s %s]\n    %s\n  %s\n\n", r.Platform, r.Account, r.Conversation, r.Date, dir, r.Line)
	}
	return nil
}
