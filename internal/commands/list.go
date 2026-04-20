package commands

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/read"
	"github.com/anish749/pigeon/internal/search"
	"github.com/anish749/pigeon/internal/store"
	"github.com/anish749/pigeon/internal/store/modelv1"
	"github.com/anish749/pigeon/internal/timeutil"
	"github.com/anish749/pigeon/internal/workspace"
)

func RunList(platform, accountName string) error {
	s := store.NewFSStore(paths.DefaultDataRoot())

	// Level 3: list conversations for a specific account
	if platform != "" && accountName != "" {
		acct := account.New(platform, accountName)
		fmt.Printf("%s:\n", acct.Display())
		return listAccount(s, acct)
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

	// Append workspace groupings if any are configured.
	cfg, err := config.Load()
	if err != nil || len(cfg.Workspaces) == 0 {
		return nil
	}

	names := make([]config.WorkspaceName, 0, len(cfg.Workspaces))
	for name := range cfg.Workspaces {
		names = append(names, name)
	}
	slices.SortFunc(names, func(a, b config.WorkspaceName) int {
		return cmp.Compare(a, b)
	})

	fmt.Println("workspaces:")
	for _, name := range names {
		ws := cfg.Workspaces[name]
		marker := ""
		if name == cfg.DefaultWorkspace {
			marker = " (default)"
		}
		fmt.Printf("\n  %s%s\n", name, marker)
		for _, slug := range ws.Slack {
			fmt.Printf("    slack/%s\n", slug)
		}
		for _, slug := range ws.GWS {
			fmt.Printf("    gws/%s\n", slug)
		}
		for _, slug := range ws.WhatsApp {
			fmt.Printf("    whatsapp/%s\n", slug)
		}
	}
	return nil
}

// RunListScoped lists accounts and conversations scoped to the given accounts.
func RunListScoped(accounts []account.Account, platform string) error {
	s := store.NewFSStore(paths.DefaultDataRoot())

	// If platform filter is set, only show accounts on that platform.
	var filtered []account.Account
	for _, acct := range accounts {
		if platform == "" || acct.Platform == platform {
			filtered = append(filtered, acct)
		}
	}
	if len(filtered) == 0 {
		fmt.Println("No accounts found.")
		return nil
	}

	for _, acct := range filtered {
		fmt.Printf("%s:\n", acct.Display())
		if err := listAccount(s, acct); err != nil {
			return err
		}
		fmt.Println()
	}
	return nil
}

// RunListSince lists conversations with activity within the given window.
func RunListSince(ws *workspace.Workspace, platform, accountName string, since time.Duration) error {
	s := store.NewFSStore(paths.DefaultDataRoot())

	dirs, err := read.SearchDirs(ws, platform, accountName)
	if err != nil {
		return err
	}

	var allFiles []string
	for _, dir := range dirs {
		files, err := read.Glob(dir, since)
		if err != nil {
			return err
		}
		allFiles = append(allFiles, files...)
	}
	if len(allFiles) == 0 {
		fmt.Println("No conversations found.")
		return nil
	}

	convs := activeConversations(allFiles, paths.DefaultDataRoot().Path())
	if len(convs) == 0 {
		fmt.Println("No conversations found.")
		return nil
	}

	now := time.Now()
	for _, conv := range convs {
		latest, err := s.LatestConversationActivity(conv.Account, conv.Name, since)
		if err != nil {
			return fmt.Errorf("read latest activity for %s: %w", conv.Display, err)
		}
		if latest.IsZero() {
			fmt.Printf("%s  active\n", conv.Display)
		} else {
			fmt.Printf("%s  last: %s ago\n", conv.Display, timeutil.FormatAge(now.Sub(latest)))
		}
		fmt.Printf("  %s\n", conv.Dir)
	}
	return nil
}

// listAccount prints a header and conversations (or GWS services) for a
// single account.
func listAccount(s *store.FSStore, acct account.Account) error {
	if acct.Platform == "gws" {
		return listGWSAccount(s, acct)
	}

	convs, err := s.ListConversations(acct)
	if err != nil {
		return err
	}
	if len(convs) == 0 {
		fmt.Println("No conversations found.")
		return nil
	}
	for _, c := range convs {
		meta, err := s.ReadMeta(acct, c)
		if err != nil {
			return fmt.Errorf("read metadata for %s: %w", c, err)
		}
		if meta != nil {
			if ids := modelv1.FormatConvMeta(meta); ids != "" {
				fmt.Printf("  %s  %s\n", c, ids)
				continue
			}
		}
		fmt.Printf("  %s\n", c)
	}
	return nil
}

type activeConversation struct {
	Account account.Account
	Name    string
	Display string
	Dir     string
}

func activeConversations(files []string, root string) []activeConversation {
	seen := make(map[string]struct{})
	var convs []activeConversation
	for _, f := range files {
		platform, acctName, convName, _, _, err := search.ParseFilePath(f, root)
		if err != nil {
			continue
		}
		key := strings.Join([]string{platform, acctName, convName}, "\x00")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		acct := account.NewFromSlug(platform, acctName)
		convs = append(convs, activeConversation{
			Account: acct,
			Name:    convName,
			Display: strings.Join([]string{platform, acctName, convName}, "/"),
			Dir:     paths.NewDataRoot(root).AccountFor(acct).Conversation(convName).Path(),
		})
	}
	return convs
}

// listGWSAccount prints the services and item counts for a GWS account.
func listGWSAccount(s *store.FSStore, acct account.Account) error {
	infos, err := s.ListGWSServices(acct)
	if err != nil {
		return err
	}
	if len(infos) == 0 {
		fmt.Println("No services found.")
		return nil
	}
	for _, info := range infos {
		fmt.Printf("  %s  %s\n", info.Service, gwsItemLabel(info.Service, info.Items))
	}
	return nil
}

// gwsItemLabel returns a human-readable count label for a GWS service.
func gwsItemLabel(service string, n int) string {
	switch service {
	case paths.GmailSubdir:
		return pluralize(n, "day", "days")
	case paths.GcalendarSubdir:
		return pluralize(n, "calendar", "calendars")
	case paths.GdriveSubdir:
		return pluralize(n, "file", "files")
	default:
		return pluralize(n, "item", "items")
	}
}

func pluralize(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
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
	case "gws":
		for _, g := range cfg.GWS {
			acct := account.New("gws", g.Email)
			canonical[acct.NameSlug()] = g.Email
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
