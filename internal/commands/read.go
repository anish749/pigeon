package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
	"github.com/anish749/pigeon/internal/store/modelv1"
	"github.com/anish749/pigeon/internal/timeutil"
)

type ReadParams struct {
	Source   string
	Selector string
	Account  string
	Context  string
	Date     string
	Last     int
	Since    string
}

func RunRead(p ReadParams) error {
	scope, err := ResolveScope(p.Source, p.Context, p.Account)
	if err != nil {
		return err
	}
	s := store.NewFSStore(paths.DefaultDataRoot())
	opts, err := readOptsForSource(scope.Source, p.Date, p.Since, p.Last)
	if err != nil {
		return err
	}

	var sections []string
	for _, acct := range scope.Accounts {
		var section string
		switch scope.Source {
		case SourceSlack, SourceWhatsApp:
			if strings.TrimSpace(p.Selector) == "" {
				return fmt.Errorf("pigeon read %s requires a selector", scope.Source)
			}
			section, err = readConversationSource(s, acct, scope.Source, p.Selector, opts)
		case SourceGmail:
			section, err = readGmailSource(acct, opts)
		case SourceCalendar:
			calID := p.Selector
			if calID == "" {
				calID = "primary"
			}
			section, err = readCalendarSource(acct, calID, opts)
		case SourceDrive:
			if strings.TrimSpace(p.Selector) == "" {
				return fmt.Errorf("pigeon read drive requires a document name")
			}
			section, err = readDriveSource(s, acct, p.Selector)
		default:
			err = fmt.Errorf("unsupported source %q", scope.Source)
		}
		if err != nil {
			return err
		}
		if section != "" {
			sections = append(sections, section)
		}
	}
	if len(sections) == 0 {
		fmt.Println("No results found.")
		return nil
	}
	fmt.Println(strings.Join(sections, "\n\n"))
	return nil
}

func readOptsForSource(source Source, date, since string, last int) (store.ReadOpts, error) {
	opts := store.ReadOpts{
		Date: date,
		Last: last,
	}
	if since != "" {
		d, err := timeutil.ParseDuration(since)
		if err != nil {
			return opts, fmt.Errorf("invalid --since value %q: %w", since, err)
		}
		opts.Since = d
	}

	if opts.Date == "" && opts.Since == 0 && opts.Last == 0 {
		today := time.Now().Format("2006-01-02")
		switch source {
		case SourceGmail:
			opts.Last = 25
		case SourceCalendar, SourceSlack, SourceWhatsApp:
			opts.Date = today
		}
	}
	return opts, nil
}

func readConversationSource(s store.Store, acct ResolvedAccount, source Source, selector string, opts store.ReadOpts) (string, error) {
	aliases := loadAliases(acct.Acct)
	conv, err := findConversation(s, acct.Acct, selector, aliases)
	if err != nil {
		return "", err
	}

	df, readErr := s.ReadConversation(acct.Acct, conv.dirName, opts)
	if df == nil || len(df.Messages) == 0 {
		if readErr != nil {
			return "", readErr
		}
		return fmt.Sprintf("--- %s/%s/%s ---\nNo messages found.", source, acct.Identifier, conv.displayName), nil
	}

	lines := modelv1.FormatDateFile(df, time.Local, readErr)
	convDir := paths.DefaultDataRoot().AccountFor(acct.Acct).Conversation(conv.dirName)
	header := fmt.Sprintf("--- %s/%s/%s ---", source, acct.Identifier, conv.displayName)
	return strings.Join(append([]string{header, "    " + convDir.Path()}, lines...), "\n"), nil
}

func readGmailSource(acct ResolvedAccount, opts store.ReadOpts) (string, error) {
	gmailDir := paths.DefaultDataRoot().AccountFor(acct.Acct).Gmail()
	files, err := selectDateFiles(gmailDir.Path(), opts)
	if err != nil {
		return "", err
	}
	emails, err := loadEmails(files, gmailDir.PendingDeletesPath())
	if err != nil {
		return "", err
	}
	if opts.Last > 0 && len(emails) > opts.Last {
		emails = emails[len(emails)-opts.Last:]
	}

	lines := []string{
		fmt.Sprintf("--- gmail/%s ---", acct.HeaderLabel()),
		"    " + gmailDir.Path(),
	}
	lines = append(lines, formatSelectedFiles(files)...)
	for _, email := range emails {
		lines = append(lines, fmt.Sprintf("[%s] [%s] %s <%s>: %s",
			email.Ts.In(time.Local).Format("2006-01-02 15:04:05"),
			email.ID,
			email.FromName,
			email.From,
			email.Subject,
		))
	}
	if len(emails) == 0 {
		lines = append(lines, "No emails found.")
	}
	return strings.Join(lines, "\n"), nil
}

func readCalendarSource(acct ResolvedAccount, calID string, opts store.ReadOpts) (string, error) {
	calDir := paths.DefaultDataRoot().AccountFor(acct.Acct).Calendar(calID)
	files, err := selectDateFiles(calDir.Path(), opts)
	if err != nil {
		return "", err
	}
	events, err := loadEvents(files)
	if err != nil {
		return "", err
	}
	lines := []string{
		fmt.Sprintf("--- calendar/%s/%s ---", acct.HeaderLabel(), calID),
		"    " + calDir.Path(),
	}
	lines = append(lines, formatSelectedFiles(files)...)
	for _, event := range events {
		start := formatEventStart(event)
		summary := strings.TrimSpace(event.Runtime.Summary)
		if summary == "" {
			summary = "(untitled event)"
		}
		lines = append(lines, fmt.Sprintf("[%s] [%s] %s", start, event.Runtime.Id, summary))
	}
	if len(events) == 0 {
		lines = append(lines, "No events found.")
	}
	return strings.Join(lines, "\n"), nil
}

func readDriveSource(s *store.FSStore, acct ResolvedAccount, selector string) (string, error) {
	driveDir := paths.DefaultDataRoot().AccountFor(acct.Acct).Drive()
	entry, err := findDriveFile(s, driveDir, selector)
	if err != nil {
		return "", err
	}

	lines := []string{
		fmt.Sprintf("--- drive/%s/%s ---", acct.HeaderLabel(), entry.DisplayTitle),
		"    " + entry.Dir.Path(),
	}
	if entry.MetaFile.Path() != "" {
		lines = append(lines, formatFileStat("meta", entry.MetaFile.Path()))
	}
	for _, path := range entry.ContentFiles {
		lines = append(lines, formatFileStat("file", path))
	}
	if entry.CommentsFile != "" {
		lines = append(lines, formatFileStat("comments", entry.CommentsFile))
	}
	return strings.Join(lines, "\n"), nil
}

func selectDateFiles(dir string, opts store.ReadOpts) ([]string, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*"+paths.FileExt))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	var selected []string
	switch {
	case opts.Date != "":
		target := filepath.Join(dir, opts.Date+paths.FileExt)
		if fileExists(target) {
			selected = []string{target}
		}
	case opts.Since > 0:
		cutoff := time.Now().Add(-opts.Since)
		for _, file := range files {
			date := strings.TrimSuffix(filepath.Base(file), paths.FileExt)
			day, err := time.Parse("2006-01-02", date)
			if err != nil {
				continue
			}
			if !day.Before(cutoff.Truncate(24 * time.Hour)) {
				selected = append(selected, file)
			}
		}
	default:
		selected = files
	}
	return selected, nil
}

func loadEmails(files []string, pendingDeletesPath string) ([]modelv1.EmailLine, error) {
	deleted, err := pendingDeleteSet(pendingDeletesPath)
	if err != nil {
		return nil, err
	}
	seen := map[string]modelv1.EmailLine{}
	for _, file := range files {
		lines, err := loadLines(file)
		if err != nil {
			return nil, err
		}
		for _, line := range lines {
			if line.Type != modelv1.LineEmail || line.Email == nil {
				continue
			}
			if deleted[line.Email.ID] {
				continue
			}
			seen[line.Email.ID] = *line.Email
		}
	}
	var emails []modelv1.EmailLine
	for _, email := range seen {
		emails = append(emails, email)
	}
	sort.Slice(emails, func(i, j int) bool { return emails[i].Ts.Before(emails[j].Ts) })
	return emails, nil
}

func pendingDeleteSet(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]bool{}, nil
		}
		return nil, err
	}
	deleted := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line != "" {
			deleted[line] = true
		}
	}
	return deleted, nil
}

func loadEvents(files []string) ([]modelv1.CalendarEvent, error) {
	seen := map[string]modelv1.CalendarEvent{}
	for _, file := range files {
		lines, err := loadLines(file)
		if err != nil {
			return nil, err
		}
		for _, line := range lines {
			if line.Type != modelv1.LineEvent || line.Event == nil {
				continue
			}
			if line.Event.Runtime.Status == "cancelled" {
				delete(seen, line.Event.Runtime.Id)
				continue
			}
			seen[line.Event.Runtime.Id] = *line.Event
		}
	}
	var events []modelv1.CalendarEvent
	for _, event := range seen {
		events = append(events, event)
	}
	sort.Slice(events, func(i, j int) bool {
		return eventStart(events[i]).Before(eventStart(events[j]))
	})
	return events, nil
}

func eventStart(event modelv1.CalendarEvent) time.Time {
	if event.Runtime.Start != nil {
		if event.Runtime.Start.DateTime != "" {
			if t, err := time.Parse(time.RFC3339, event.Runtime.Start.DateTime); err == nil {
				return t
			}
		}
		if event.Runtime.Start.Date != "" {
			if t, err := time.Parse("2006-01-02", event.Runtime.Start.Date); err == nil {
				return t
			}
		}
	}
	return time.Time{}
}

func formatEventStart(event modelv1.CalendarEvent) string {
	start := eventStart(event)
	if start.IsZero() {
		return "unknown"
	}
	if event.Runtime.Start != nil && event.Runtime.Start.Date != "" && event.Runtime.Start.DateTime == "" {
		return start.Format("2006-01-02")
	}
	return start.In(time.Local).Format("2006-01-02 15:04")
}

type driveFileMatch struct {
	Dir          paths.DriveFileDir
	MetaFile     paths.DriveMetaFile
	DisplayTitle string
	ContentFiles []string
	CommentsFile string
}

func findDriveFile(s *store.FSStore, driveDir paths.DriveDir, selector string) (*driveFileMatch, error) {
	entries, err := os.ReadDir(driveDir.Path())
	if err != nil {
		return nil, err
	}
	query := strings.ToLower(strings.TrimSpace(selector))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := driveDir.File(entry.Name())
		metaFile, meta, _ := latestDriveMeta(s, dir)
		title := entry.Name()
		if meta != nil && meta.Title != "" {
			title = meta.Title
		}
		if !driveSelectorMatches(entry.Name(), title, query) {
			continue
		}
		content, err := collectDriveContent(dir)
		if err != nil {
			return nil, err
		}
		return &driveFileMatch{
			Dir:          dir,
			MetaFile:     metaFile,
			DisplayTitle: title,
			ContentFiles: content,
			CommentsFile: dir.CommentsFile().Path(),
		}, nil
	}
	return nil, fmt.Errorf("no drive file matching %q", selector)
}

func latestDriveMeta(s *store.FSStore, dir paths.DriveFileDir) (paths.DriveMetaFile, *modelv1.DocMeta, error) {
	matches, err := filepath.Glob(filepath.Join(dir.Path(), paths.DriveMetaFileGlob))
	if err != nil || len(matches) == 0 {
		return paths.DriveMetaFile{}, nil, err
	}
	sort.Strings(matches)
	metaFile, ok, err := paths.ParseDriveMetaPath(matches[len(matches)-1])
	if err != nil || !ok {
		return paths.DriveMetaFile{}, nil, err
	}
	meta, err := s.LoadDriveMeta(metaFile)
	if err != nil {
		return metaFile, nil, err
	}
	return metaFile, meta, nil
}

func collectDriveContent(dir paths.DriveFileDir) ([]string, error) {
	entries, err := os.ReadDir(dir.Path())
	if err != nil {
		return nil, err
	}
	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(dir.Path(), entry.Name())
		if path == dir.CommentsFile().Path() || strings.HasPrefix(entry.Name(), "drive-meta-") {
			continue
		}
		files = append(files, path)
	}
	sort.Strings(files)
	return files, nil
}

func driveSelectorMatches(dirName, title, query string) bool {
	name := strings.ToLower(dirName)
	title = strings.ToLower(title)
	return name == query || title == query || strings.Contains(name, query) || strings.Contains(title, query)
}

func formatSelectedFiles(files []string) []string {
	if len(files) == 0 {
		return []string{"    no backing files selected"}
	}
	lines := []string{"    files:"}
	for _, file := range files {
		lines = append(lines, "    "+formatFileStat("file", file))
	}
	return lines
}

func formatFileStat(kind, path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Sprintf("%s: %s", kind, path)
	}
	lineCount := countLines(path)
	return fmt.Sprintf("%s: %s (%d bytes, %d lines)", kind, path, info.Size(), lineCount)
}

func countLines(path string) int {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return 0
	}
	return strings.Count(string(data), "\n")
}

func loadLines(path string) ([]modelv1.Line, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var lines []modelv1.Line
	for _, raw := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if raw == "" {
			continue
		}
		line, err := modelv1.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		lines = append(lines, line)
	}
	return lines, nil
}

// conversation holds directory and display info for a matched conversation.
type conversation struct {
	dirName     string
	displayName string
}

// findConversation searches for a conversation matching the query by directory
// name, display name, or alias. Matching is exact and case-insensitive; older
// directory naming conventions still resolve through parseDisplayName.
func findConversation(s store.Store, acct account.Account, query string, aliases map[string][]string) (*conversation, error) {
	convs, err := s.ListConversations(acct)
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(strings.TrimSpace(query))
	for _, dirName := range convs {
		displayName := parseDisplayName(dirName)
		if strings.EqualFold(dirName, q) || strings.EqualFold(displayName, q) {
			return &conversation{dirName: dirName, displayName: displayName}, nil
		}
		for _, alias := range aliases[dirName] {
			if strings.EqualFold(alias, q) {
				return &conversation{dirName: dirName, displayName: displayName}, nil
			}
		}
	}
	for _, dirName := range convs {
		displayName := parseDisplayName(dirName)
		if strings.Contains(strings.ToLower(dirName), q) || strings.Contains(strings.ToLower(displayName), q) {
			return &conversation{dirName: dirName, displayName: displayName}, nil
		}
	}
	return nil, fmt.Errorf("no conversation matching %q in %s", query, acct.Display())
}

func parseDisplayName(dirName string) string {
	if idx := strings.Index(dirName, "_"); idx > 0 {
		return dirName[idx+1:]
	}
	return dirName
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func prettyJSONLength(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return 0, err
	}
	return len(data), nil
}

func sortUniqueStrings(items []string) []string {
	var out []string
	for _, item := range items {
		if slices.Contains(out, item) {
			continue
		}
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}
