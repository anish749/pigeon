package commands

import (
	"fmt"
	"strings"

	"github.com/anish/claude-msg-utils/internal/config"
	"github.com/anish/claude-msg-utils/internal/store"
)

func RunList(platform, account string) error {
	// Level 3: list conversations for a specific account
	if platform != "" && account != "" {
		aliases := loadAliases(platform, account)
		convs, err := store.ListConversations(platform, account, aliases)
		if err != nil {
			return err
		}
		if len(convs) == 0 {
			fmt.Println("No conversations found.")
			return nil
		}
		fmt.Printf("Conversations in %s/%s:\n\n", platform, account)
		for _, c := range convs {
			fmt.Printf("  %-20s  %s\n", c.Identifier, c.DisplayName)
		}
		return nil
	}

	// Level 2: list accounts for a specific platform
	if platform != "" {
		accounts, err := store.ListAccounts(platform)
		if err != nil {
			return err
		}
		if len(accounts) == 0 {
			fmt.Println("No accounts found.")
			return nil
		}
		accounts = canonicalAccountNames(platform, accounts)
		fmt.Printf("Accounts in %s:\n\n", platform)
		for _, a := range accounts {
			fmt.Printf("  %s\n", a)
		}
		return nil
	}

	// Level 1: list all platforms and their accounts
	platforms, err := store.ListPlatforms()
	if err != nil {
		return fmt.Errorf("cannot read data directory %s: %w", store.DataDir(), err)
	}
	if len(platforms) == 0 {
		fmt.Printf("No platforms found in %s\n", store.DataDir())
		return nil
	}
	for _, p := range platforms {
		accounts, err := store.ListAccounts(p)
		if err != nil {
			continue
		}
		accounts = canonicalAccountNames(p, accounts)
		fmt.Printf("%s:\n", p)
		for _, a := range accounts {
			fmt.Printf("  %s\n", a)
		}
		fmt.Println()
	}
	return nil
}

// canonicalAccountNames replaces filesystem directory names with canonical names
// from config. The filesystem is case-insensitive on macOS, so directory names
// may not match the canonical casing from the Slack API / config file.
func canonicalAccountNames(platform string, dirNames []string) []string {
	cfg, err := config.Load()
	if err != nil {
		return dirNames
	}

	var canonical map[string]string // lowercase dir name → config name
	switch platform {
	case "slack":
		canonical = make(map[string]string, len(cfg.Slack))
		for _, sl := range cfg.Slack {
			canonical[strings.ToLower(sl.Workspace)] = sl.Workspace
		}
	case "whatsapp":
		canonical = make(map[string]string, len(cfg.WhatsApp))
		for _, wa := range cfg.WhatsApp {
			canonical[strings.ToLower(wa.Account)] = wa.Account
		}
	default:
		return dirNames
	}

	result := make([]string, len(dirNames))
	for i, dir := range dirNames {
		if name, ok := canonical[strings.ToLower(dir)]; ok {
			result[i] = name
		} else {
			result[i] = dir
		}
	}
	return result
}
