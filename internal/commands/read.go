package commands

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/anish/claude-msg-utils/internal/store"
)

type ReadParams struct {
	Platform string
	Account  string
	Contact  string
	Date     string
	Last     int
	Since    string
}

func RunRead(p ReadParams) error {
	aliases := loadAliases(p.Platform, p.Account)
	conv, err := store.FindConversation(p.Platform, p.Account, p.Contact, aliases)
	if err != nil {
		return err
	}

	opts := store.ReadOpts{
		Date: p.Date,
		Last: p.Last,
	}
	if p.Since != "" {
		d, err := parseDuration(p.Since)
		if err != nil {
			return fmt.Errorf("invalid --since value %q: %w", p.Since, err)
		}
		opts.Since = d
	}

	lines, err := store.ReadMessages(p.Platform, p.Account, conv.DirName, opts)
	if err != nil {
		return err
	}

	// Interleave thread replies for Slack conversations, and append
	// threads with recent activity whose parent is outside the time window.
	if p.Platform == "slack" {
		lines = store.InterleaveThreads(p.Platform, p.Account, conv.DirName, lines)
		if opts.Since > 0 {
			lines = store.AppendActiveThreads(p.Platform, p.Account, conv.DirName, lines, opts.Since)
		}
	}

	if len(lines) == 0 {
		fmt.Println("No messages found.")
		return nil
	}

	lines = enrichLines(lines, aliases)

	dir := filepath.Join(store.DataDir(), p.Platform, p.Account, conv.DirName)
	fmt.Printf("--- %s/%s/%s ---\n", p.Platform, p.Account, conv.DisplayName)
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
