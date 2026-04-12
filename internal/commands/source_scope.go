package commands

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
)

type Source string

const (
	SourceGmail    Source = "gmail"
	SourceCalendar Source = "calendar"
	SourceDrive    Source = "drive"
	SourceSlack    Source = "slack"
	SourceWhatsApp Source = "whatsapp"
)

var allSources = []Source{
	SourceGmail,
	SourceCalendar,
	SourceDrive,
	SourceSlack,
	SourceWhatsApp,
}

type ResolvedAccount struct {
	Platform   string
	Identifier string
	Label      string
	Acct       account.Account
}

func (a ResolvedAccount) HeaderLabel() string {
	if a.Label != "" && a.Label != a.Identifier {
		return fmt.Sprintf("%s (%s)", a.Label, a.Identifier)
	}
	return a.Identifier
}

func (a ResolvedAccount) matches(query string) bool {
	if query == "" {
		return true
	}
	return query == a.Identifier || query == a.Label || query == a.Acct.NameSlug()
}

type ResolvedScope struct {
	ContextName string
	Source      Source
	Accounts    []ResolvedAccount
}

func ResolveScopes(sourceName, contextName, accountFilter string) ([]ResolvedScope, error) {
	if strings.TrimSpace(sourceName) != "" {
		scope, err := ResolveScope(sourceName, contextName, accountFilter)
		if err != nil {
			return nil, err
		}
		return []ResolvedScope{*scope}, nil
	}

	var (
		scopes []ResolvedScope
		errs   []error
	)
	for _, source := range allSources {
		scope, err := ResolveScope(string(source), contextName, accountFilter)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		scopes = append(scopes, *scope)
	}
	if len(scopes) == 0 {
		return nil, errors.Join(errs...)
	}
	return scopes, nil
}

func ParseSource(raw string) (Source, error) {
	s := Source(strings.ToLower(strings.TrimSpace(raw)))
	if slices.Contains(allSources, s) {
		return s, nil
	}
	return "", fmt.Errorf("unknown source %q — valid sources: gmail, calendar, drive, slack, whatsapp", raw)
}

func ResolveScope(source, contextName, accountFilter string) (*ResolvedScope, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	src, err := ParseSource(source)
	if err != nil {
		return nil, err
	}

	store := store.NewFSStore(paths.DefaultDataRoot())
	activeContext := resolveContextName(cfg, contextName)
	accounts, err := resolveAccounts(store, cfg, src, activeContext, accountFilter)
	if err != nil {
		return nil, err
	}
	if len(accounts) == 0 {
		if activeContext != "" {
			return nil, fmt.Errorf("no %s accounts resolved in context %q", sourcePlatform(src), activeContext)
		}
		return nil, fmt.Errorf("no accounts found for source %q", source)
	}
	return &ResolvedScope{
		ContextName: activeContext,
		Source:      src,
		Accounts:    accounts,
	}, nil
}

func resolveContextName(cfg *config.Config, explicit string) string {
	if explicit != "" {
		return explicit
	}
	if env := os.Getenv("PIGEON_CONTEXT"); env != "" {
		return env
	}
	return cfg.DefaultContext
}

func resolveAccounts(s *store.FSStore, cfg *config.Config, source Source, contextName, accountFilter string) ([]ResolvedAccount, error) {
	platform := sourcePlatform(source)
	all, err := allAccountsForPlatform(s, cfg, platform)
	if err != nil {
		return nil, err
	}
	if contextName != "" {
		ctx, ok := cfg.Contexts[contextName]
		if !ok {
			return nil, fmt.Errorf("unknown context %q", contextName)
		}
		keys := contextForeignKeys(ctx, platform)
		var filtered []ResolvedAccount
		for _, acct := range all {
			if slices.ContainsFunc(keys, func(key string) bool { return acct.matches(key) }) {
				filtered = append(filtered, acct)
			}
		}
		all = filtered
	}
	if accountFilter == "" {
		return all, nil
	}
	var filtered []ResolvedAccount
	for _, acct := range all {
		if acct.matches(accountFilter) {
			filtered = append(filtered, acct)
		}
	}
	if len(filtered) == 0 {
		return nil, fmt.Errorf("no account matching %q for source %q", accountFilter, source)
	}
	return filtered, nil
}

func contextForeignKeys(ctx config.ContextConfig, platform string) []string {
	switch platform {
	case "slack":
		return []string(ctx.Slack)
	case "whatsapp":
		return []string(ctx.WhatsApp)
	case "gws":
		return []string(ctx.GWS)
	default:
		return nil
	}
}

func allAccountsForPlatform(s *store.FSStore, cfg *config.Config, platform string) ([]ResolvedAccount, error) {
	known := map[string]ResolvedAccount{}
	add := func(acct ResolvedAccount) {
		known[acct.Acct.NameSlug()] = acct
	}

	switch platform {
	case "slack":
		for _, sl := range cfg.Slack {
			acct := account.New("slack", sl.Workspace)
			add(ResolvedAccount{Platform: "slack", Identifier: sl.Workspace, Label: sl.Workspace, Acct: acct})
		}
	case "whatsapp":
		for _, wa := range cfg.WhatsApp {
			acct := account.New("whatsapp", wa.Account)
			add(ResolvedAccount{Platform: "whatsapp", Identifier: wa.Account, Label: wa.Account, Acct: acct})
		}
	case "gws":
		for _, g := range cfg.GWS {
			acct := account.New("gws", g.Email)
			add(ResolvedAccount{Platform: "gws", Identifier: g.Email, Label: g.Account, Acct: acct})
		}
	}

	slugs, err := s.ListAccounts(platform)
	if err != nil {
		return nil, err
	}
	for _, slug := range slugs {
		if _, ok := known[slug]; ok {
			continue
		}
		acct := account.NewFromSlug(platform, slug)
		add(ResolvedAccount{Platform: platform, Identifier: slug, Label: slug, Acct: acct})
	}

	var accounts []ResolvedAccount
	for _, acct := range known {
		accounts = append(accounts, acct)
	}
	slices.SortFunc(accounts, func(a, b ResolvedAccount) int {
		return strings.Compare(a.Identifier, b.Identifier)
	})
	return accounts, nil
}

func sourcePlatform(source Source) string {
	switch source {
	case SourceSlack:
		return "slack"
	case SourceWhatsApp:
		return "whatsapp"
	default:
		return "gws"
	}
}

func sourceRoots(acct ResolvedAccount, source Source) []string {
	accountDir := paths.DefaultDataRoot().AccountFor(acct.Acct)
	switch source {
	case SourceGmail:
		return []string{accountDir.Gmail().Path()}
	case SourceCalendar:
		return []string{filepath.Join(accountDir.Path(), "gcalendar")}
	case SourceDrive:
		return []string{accountDir.Drive().Path()}
	case SourceSlack, SourceWhatsApp:
		return []string{accountDir.Path()}
	default:
		return nil
	}
}

func SourceRootsForCLI(acct ResolvedAccount, source Source) []string {
	return sourceRoots(acct, source)
}
