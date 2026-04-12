package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/read"
	"github.com/anish749/pigeon/internal/store"
	"github.com/anish749/pigeon/internal/timeutil"
)

func RunList(source, accountName, context string) error {
	if source == "" && context == "" && accountName == "" {
		return runLegacyAccountList()
	}

	scopes, err := ResolveScopes(source, context, accountName)
	if err != nil {
		return err
	}
	s := store.NewFSStore(paths.DefaultDataRoot())
	var lines []string
	if scopes[0].ContextName != "" {
		lines = append(lines, fmt.Sprintf("Sources for %s:", scopes[0].ContextName), "")
	}
	for _, scope := range scopes {
		for _, acct := range scope.Accounts {
			resourceLines, err := listResourcesForScope(s, scope.Source, acct)
			if err != nil {
				return err
			}
			lines = append(lines, resourceLines...)
		}
	}
	if len(lines) == 0 {
		fmt.Println("No sources found.")
		return nil
	}
	fmt.Println(strings.Join(lines, "\n"))
	return nil
}

func RunListSince(source, accountName, context, since string) error {
	sinceDur, err := timeutil.ParseDuration(since)
	if err != nil {
		return fmt.Errorf("invalid --since value %q: %w", since, err)
	}
	scopes, err := ResolveScopes(source, context, accountName)
	if err != nil {
		return err
	}
	files, err := read.GlobMany(scopeRoots(scopes), sinceDur)
	if err != nil {
		return err
	}
	resources := extractResources(files, paths.DefaultDataRoot().Path())
	if len(resources) == 0 {
		fmt.Println("No sources found.")
		return nil
	}

	now := time.Now()
	for _, resource := range resources {
		label := resource.Display
		if resource.LatestDate != "" {
			t, _ := time.Parse("2006-01-02", resource.LatestDate)
			label += "  last: " + timeutil.FormatAge(now.Sub(t)) + " ago"
		}
		fmt.Println(label)
		fmt.Printf("  %s\n", resource.Dir)
	}
	return nil
}

func runLegacyAccountList() error {
	s := store.NewFSStore(paths.DefaultDataRoot())
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
		fmt.Printf("%s:\n", p)
		for _, a := range accounts {
			fmt.Printf("  %s\n", a)
		}
		fmt.Println()
	}
	return nil
}

func listResourcesForScope(s *store.FSStore, source Source, acct ResolvedAccount) ([]string, error) {
	switch source {
	case SourceGmail:
		return []string{fmt.Sprintf("  gmail                                  %s", acct.HeaderLabel())}, nil
	case SourceCalendar:
		return listCalendarResources(acct)
	case SourceDrive:
		return listDriveResources(s, acct)
	case SourceSlack, SourceWhatsApp:
		return listConversationResources(s, source, acct)
	default:
		return nil, fmt.Errorf("unsupported source %q", source)
	}
}

func listCalendarResources(acct ResolvedAccount) ([]string, error) {
	accountDir := paths.DefaultDataRoot().AccountFor(acct.Acct)
	entries, err := os.ReadDir(filepath.Join(accountDir.Path(), "gcalendar"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var lines []string
	for _, entry := range entries {
		if entry.IsDir() {
			lines = append(lines, fmt.Sprintf("  calendar/%s                         %s", entry.Name(), acct.HeaderLabel()))
		}
	}
	return lines, nil
}

func listDriveResources(s *store.FSStore, acct ResolvedAccount) ([]string, error) {
	driveDir := paths.DefaultDataRoot().AccountFor(acct.Acct).Drive()
	entries, err := os.ReadDir(driveDir.Path())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var lines []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := driveDir.File(entry.Name())
		_, meta, _ := latestDriveMeta(s, dir)
		title := entry.Name()
		kind := "Drive file"
		if meta != nil {
			if meta.Title != "" {
				title = meta.Title
			}
			switch {
			case strings.Contains(meta.MimeType, "spreadsheet"):
				kind = "Google Sheet"
			case strings.Contains(meta.MimeType, "document"):
				kind = "Google Doc"
			}
		}
		lines = append(lines, fmt.Sprintf("  drive/%s                      %q (%s)", entry.Name(), title, kind))
	}
	return lines, nil
}

func listConversationResources(s store.Store, source Source, acct ResolvedAccount) ([]string, error) {
	convs, err := s.ListConversations(acct.Acct)
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, conv := range convs {
		lines = append(lines, fmt.Sprintf("  %s/%s                             %s", source, conv, acct.HeaderLabel()))
	}
	return lines, nil
}

func scopeRoots(scopes []ResolvedScope) []string {
	var roots []string
	for _, scope := range scopes {
		for _, acct := range scope.Accounts {
			roots = append(roots, sourceRoots(acct, scope.Source)...)
		}
	}
	sort.Strings(roots)
	return roots
}

type activeResource struct {
	Display    string
	Dir        string
	LatestDate string
}

func extractResources(files []string, root string) []activeResource {
	seen := make(map[string]*activeResource)
	var order []string
	for _, file := range files {
		rel, err := filepath.Rel(root, file)
		if err != nil {
			continue
		}
		parts := strings.Split(rel, string(filepath.Separator))
		resourceKey, display, dir, date := resourceFromParts(root, parts, file)
		if resourceKey == "" {
			continue
		}
		resource, ok := seen[resourceKey]
		if !ok {
			resource = &activeResource{Display: display, Dir: dir}
			seen[resourceKey] = resource
			order = append(order, resourceKey)
		}
		if date > resource.LatestDate {
			resource.LatestDate = date
		}
	}
	result := make([]activeResource, len(order))
	for i, key := range order {
		result[i] = *seen[key]
	}
	return result
}

func resourceFromParts(root string, parts []string, fullPath string) (key, display, dir, date string) {
	if len(parts) < 3 {
		return "", "", "", ""
	}
	switch parts[0] {
	case "slack", "whatsapp":
		if paths.IsThreadFile(fullPath) {
			dir = filepath.Join(root, parts[0], parts[1], parts[2])
		} else {
			dir = filepath.Dir(fullPath)
		}
		return dir, strings.Join(parts[:3], "/"), dir, dateFromPath(fullPath)
	case "gws":
		if len(parts) < 4 {
			return "", "", "", ""
		}
		switch parts[2] {
		case "gmail":
			dir = filepath.Join(root, parts[0], parts[1], parts[2])
			return dir, "gmail/" + parts[1], dir, dateFromPath(fullPath)
		case "gcalendar":
			if len(parts) < 5 {
				return "", "", "", ""
			}
			dir = filepath.Join(root, parts[0], parts[1], parts[2], parts[3])
			return dir, "calendar/" + parts[1] + "/" + parts[3], dir, dateFromPath(fullPath)
		case "gdrive":
			if len(parts) < 5 {
				return "", "", "", ""
			}
			dir = filepath.Join(root, parts[0], parts[1], parts[2], parts[3])
			return dir, "drive/" + parts[1] + "/" + parts[3], dir, dateFromPath(fullPath)
		}
	}
	return "", "", "", ""
}

func dateFromPath(path string) string {
	base := filepath.Base(path)
	if strings.HasSuffix(base, paths.FileExt) && paths.IsDateFile(base) {
		return strings.TrimSuffix(base, paths.FileExt)
	}
	if strings.HasPrefix(base, "drive-meta-") && strings.HasSuffix(base, ".json") {
		return strings.TrimSuffix(strings.TrimPrefix(base, "drive-meta-"), ".json")
	}
	return ""
}
