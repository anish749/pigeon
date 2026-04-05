package commands

import (
	"fmt"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
)

func RunList(platform, accountName string) error {
	// Level 3: list conversations for a specific account
	if platform != "" && accountName != "" {
		acct := account.New(platform, accountName)
		aliases := loadAliases(acct)
		convs, err := store.ListConversations(acct.Platform, acct.NameSlug(), aliases)
		if err != nil {
			return err
		}
		if len(convs) == 0 {
			fmt.Println("No conversations found.")
			return nil
		}
		fmt.Printf("Conversations in %s:\n\n", acct.Display())
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
		return fmt.Errorf("cannot read data directory %s: %w", paths.DataDir(), err)
	}
	if len(platforms) == 0 {
		fmt.Printf("No platforms found in %s\n", paths.DataDir())
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

// canonicalAccountNames replaces filesystem directory names (slugs) with
// display names from config.
func canonicalAccountNames(platform string, dirNames []string) []string {
	cfg, err := config.Load()
	if err != nil {
		return dirNames
	}

	canonical := make(map[string]string) // slug → display name
	switch platform {
	case "slack":
		for _, sl := range cfg.Slack {
			acct := account.New("slack", sl.Workspace)
			canonical[acct.NameSlug()] = sl.Workspace
		}
	case "whatsapp":
		for _, wa := range cfg.WhatsApp {
			acct := account.New("whatsapp", wa.Account)
			canonical[acct.NameSlug()] = wa.Account
		}
	default:
		return dirNames
	}

	result := make([]string, len(dirNames))
	for i, dir := range dirNames {
		if name, ok := canonical[dir]; ok {
			result[i] = name
		} else {
			result[i] = dir
		}
	}
	return result
}
