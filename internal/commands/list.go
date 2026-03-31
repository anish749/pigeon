package commands

import (
	"flag"
	"fmt"

	"github.com/anish/claude-msg-utils/internal/daemon"
	"github.com/anish/claude-msg-utils/internal/store"
)

func RunList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	platform := fs.String("platform", "", "filter by platform (e.g. whatsapp, slack)")
	account := fs.String("account", "", "filter by account (e.g. +14155551234, acme-corp)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	daemon.EnsureRunning()

	// Level 3: list conversations for a specific account
	if *platform != "" && *account != "" {
		aliases := loadAliases(*platform, *account)
		convs, err := store.ListConversations(*platform, *account, aliases)
		if err != nil {
			return err
		}
		if len(convs) == 0 {
			fmt.Println("No conversations found.")
			return nil
		}
		fmt.Printf("Conversations in %s/%s:\n\n", *platform, *account)
		for _, c := range convs {
			fmt.Printf("  %-20s  %s\n", c.Identifier, c.DisplayName)
		}
		return nil
	}

	// Level 2: list accounts for a specific platform
	if *platform != "" {
		accounts, err := store.ListAccounts(*platform)
		if err != nil {
			return err
		}
		if len(accounts) == 0 {
			fmt.Println("No accounts found.")
			return nil
		}
		fmt.Printf("Accounts in %s:\n\n", *platform)
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
		fmt.Printf("%s:\n", p)
		for _, a := range accounts {
			fmt.Printf("  %s\n", a)
		}
		fmt.Println()
	}
	return nil
}
