package commands

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	acctpkg "github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/paths"
)

// RunUnlinkSlack deletes message data and removes the Slack workspace from
// config. This is the inverse of setup-slack.
func RunUnlinkSlack(account string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Find the matching config entry (or the only one).
	var sl *config.SlackConfig
	if account != "" {
		for i := range cfg.Slack {
			if cfg.Slack[i].Workspace == account {
				sl = &cfg.Slack[i]
				break
			}
		}
		if sl == nil {
			return fmt.Errorf("no Slack account %q in config", account)
		}
	} else if len(cfg.Slack) == 1 {
		sl = &cfg.Slack[0]
	} else if len(cfg.Slack) == 0 {
		return fmt.Errorf("no Slack accounts configured")
	} else {
		var names []string
		for _, s := range cfg.Slack {
			names = append(names, s.Workspace)
		}
		return fmt.Errorf("multiple accounts configured, specify --account:\n  %s", strings.Join(names, "\n  "))
	}

	workspace := sl.Workspace

	// Delete message data.
	dataDir := paths.DefaultDataRoot().AccountFor(acctpkg.New("slack", workspace)).Path()
	if err := os.RemoveAll(dataDir); err != nil {
		slog.Warn("failed to delete message data", "dir", dataDir, "error", err)
	} else {
		fmt.Printf("Deleted message data: %s\n", dataDir)
	}

	// Remove config entry.
	cfg.RemoveSlack(workspace)
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	fmt.Printf("Removed %s from config.\n", workspace)

	return nil
}
