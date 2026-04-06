package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/api"
	"github.com/anish749/pigeon/internal/config"
	daemonclient "github.com/anish749/pigeon/internal/daemon/client"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
)

func RunList(platform, accountName string) error {
	s := store.NewFSStore(paths.DefaultDataRoot())

	// Level 3: list conversations for a specific account
	if platform != "" && accountName != "" {
		acct := account.New(platform, accountName)
		convs, err := listConversations(acct, s)
		if err != nil {
			return err
		}
		if len(convs) == 0 {
			fmt.Println("No conversations found.")
			return nil
		}
		fmt.Printf("Conversations in %s:\n\n", acct.Display())
		for _, c := range convs {
			if c.UserID != "" {
				fmt.Printf("  %s  %s\n", c.Name, c.UserID)
			} else {
				fmt.Printf("  %s\n", c.Name)
			}
		}
		return nil
	}

	// Level 2: list accounts for a specific platform
	if platform != "" {
		accounts, err := s.ListAccounts(platform)
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
	platforms, err := s.ListPlatforms()
	if err != nil {
		return fmt.Errorf("cannot read data directory %s: %w", paths.DataDir(), err)
	}
	if len(platforms) == 0 {
		fmt.Printf("No platforms found in %s\n", paths.DataDir())
		return nil
	}
	for _, p := range platforms {
		accounts, err := s.ListAccounts(p)
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

// listConversations tries the daemon API first (which includes user IDs for DMs),
// falling back to the filesystem store.
func listConversations(acct account.Account, s store.Store) ([]api.ConversationInfo, error) {
	convs, err := listConversationsFromDaemon(acct)
	if err == nil {
		return convs, nil
	}

	// Fallback: read from disk (no user IDs available).
	names, err := s.ListConversations(acct)
	if err != nil {
		return nil, err
	}
	result := make([]api.ConversationInfo, len(names))
	for i, name := range names {
		result[i] = api.ConversationInfo{Name: name}
	}
	return result, nil
}

func listConversationsFromDaemon(acct account.Account) ([]api.ConversationInfo, error) {
	client := daemonclient.DefaultPgnHTTPClient
	u := fmt.Sprintf("http://pigeon/api/conversations?platform=%s&account=%s",
		url.QueryEscape(acct.Platform), url.QueryEscape(acct.Name))
	resp, err := client.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("daemon returned %d: %s", resp.StatusCode, data)
	}

	var convs []api.ConversationInfo
	if err := json.Unmarshal(data, &convs); err != nil {
		return nil, err
	}
	return convs, nil
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
