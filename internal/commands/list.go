package commands

import (
	"fmt"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
	"github.com/anish749/pigeon/internal/store/modelv1"
)

type ListParams struct {
	Platform string
	Account  string
	Since    string
}

func RunList(p ListParams) error {
	s := store.NewFSStore(paths.DefaultDataRoot())

	opts := store.ListOpts{
		Platform: p.Platform,
		Account:  p.Account,
	}
	if p.Since != "" {
		dur, err := parseDuration(p.Since)
		if err != nil {
			return fmt.Errorf("invalid --since value %q: %w", p.Since, err)
		}
		opts.Since = dur
	}

	convs, err := s.ListConversations(opts)
	if err != nil {
		return err
	}
	if len(convs) == 0 {
		fmt.Println("No conversations found.")
		return nil
	}

	// Build slug → display name mapping from config.
	canonical := canonicalAccountNames()

	if opts.Since > 0 {
		printActivityList(convs, canonical)
	} else {
		printGroupedList(s, convs, canonical)
	}
	return nil
}

// printGroupedList prints conversations grouped by platform/account, like the
// original list command.
func printGroupedList(s *store.FSStore, convs []store.ConversationInfo, canonical map[string]string) {
	type groupKey struct{ platform, account string }
	var order []groupKey
	groups := make(map[groupKey][]store.ConversationInfo)
	for _, c := range convs {
		k := groupKey{c.Platform, c.Account}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], c)
	}

	for _, k := range order {
		acctDisplay := k.account
		if name, ok := canonical[k.platform+"/"+k.account]; ok {
			acctDisplay = name
		}
		fmt.Printf("%s/%s:\n", k.platform, acctDisplay)
		for _, c := range groups[k] {
			acct := account.NewFromSlug(c.Platform, c.Account)
			meta, err := s.ReadMeta(acct, c.Conversation)
			if err == nil && meta != nil {
				if ids := modelv1.FormatConvMeta(meta); ids != "" {
					fmt.Printf("  %s  %s\n", c.Conversation, ids)
					continue
				}
			}
			fmt.Printf("  %s\n", c.Conversation)
		}
		fmt.Println()
	}
}

// printActivityList prints conversations sorted by last activity with relative
// timestamps and file paths for direct access.
func printActivityList(convs []store.ConversationInfo, canonical map[string]string) {
	now := time.Now()
	for _, c := range convs {
		acctDisplay := c.Account
		if name, ok := canonical[c.Platform+"/"+c.Account]; ok {
			acctDisplay = name
		}
		age := now.Sub(c.LastModified)
		fmt.Printf("%s/%s/%s  last: %s ago\n", c.Platform, acctDisplay, c.Conversation, formatAge(age))
		fmt.Printf("  %s\n", c.Dir)
	}
}

func formatAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		if m == 0 {
			return fmt.Sprintf("%dh", h)
		}
		return fmt.Sprintf("%dh%dm", h, m)
	default:
		return fmt.Sprintf("%dd", int(d.Hours())/24)
	}
}

// canonicalAccountNames builds a mapping from "platform/slug" → display name
// using the config file.
func canonicalAccountNames() map[string]string {
	cfg, err := config.Load()
	if err != nil {
		return nil
	}
	m := make(map[string]string)
	for _, sl := range cfg.Slack {
		acct := account.New("slack", sl.Workspace)
		m["slack/"+acct.NameSlug()] = sl.Workspace
	}
	for _, wa := range cfg.WhatsApp {
		acct := account.New("whatsapp", wa.Account)
		m["whatsapp/"+acct.NameSlug()] = wa.Account
	}
	return m
}
