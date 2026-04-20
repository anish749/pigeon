// Package reader reads all signals from the pigeon data store chronologically.
// It is used by the replay engine to feed historical data through the routing model.
package reader

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/workstream/models"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
	"github.com/anish749/pigeon/internal/store/modelv1"
)

// Reader reads all signals from the pigeon data store chronologically.
type Reader struct {
	store *store.FSStore
	root  paths.DataRoot
}

// New creates a signal reader.
func New(s *store.FSStore, root paths.DataRoot) *Reader {
	return &Reader{store: s, root: root}
}

// ReadAccounts reads signals for the given accounts within the time range.
// Returns them sorted by timestamp.
func (r *Reader) ReadAccounts(accounts []account.Account, since, until time.Time) ([]models.Signal, error) {
	var signals []models.Signal
	var errs []error

	for _, acct := range accounts {
		sigs, err := r.readAccount(acct, since, until)
		if err != nil {
			errs = append(errs, fmt.Errorf("read %s: %w", acct.Display(), err))
		}
		signals = append(signals, sigs...)
	}

	sort.Slice(signals, func(i, j int) bool {
		return signals[i].Ts.Before(signals[j].Ts)
	})

	return signals, errors.Join(errs...)
}

// ReadAll reads all signals within the given time range across all platforms
// and workspaces. Returns them sorted by timestamp.
func (r *Reader) ReadAll(since, until time.Time) ([]models.Signal, error) {
	platforms, err := r.store.ListPlatforms()
	if err != nil {
		return nil, fmt.Errorf("list platforms: %w", err)
	}

	var signals []models.Signal
	var errs []error

	for _, platform := range platforms {
		acctNames, err := r.store.ListAccounts(platform)
		if err != nil {
			errs = append(errs, fmt.Errorf("list accounts for %s: %w", platform, err))
			continue
		}
		for _, acctName := range acctNames {
			acct := account.NewFromSlug(platform, acctName)
			sigs, err := r.readAccount(acct, since, until)
			if err != nil {
				errs = append(errs, fmt.Errorf("read %s: %w", acct.Display(), err))
			}
			signals = append(signals, sigs...)
		}
	}

	sort.Slice(signals, func(i, j int) bool {
		return signals[i].Ts.Before(signals[j].Ts)
	})

	return signals, errors.Join(errs...)
}

// readAccount reads all signals for a single account across conversations
// and GWS/Linear services.
func (r *Reader) readAccount(acct account.Account, since, until time.Time) ([]models.Signal, error) {
	var signals []models.Signal
	var errs []error

	switch acct.Platform {
	case "slack", "whatsapp":
		sigs, err := r.readConversations(acct, since, until)
		if err != nil {
			errs = append(errs, err)
		}
		signals = append(signals, sigs...)

	case "gws":
		sigs, err := r.readGmail(acct, since, until)
		if err != nil {
			errs = append(errs, err)
		}
		signals = append(signals, sigs...)

		sigs, err = r.readCalendars(acct, since, until)
		if err != nil {
			errs = append(errs, err)
		}
		signals = append(signals, sigs...)

		sigs, err = r.readDriveComments(acct, since, until)
		if err != nil {
			errs = append(errs, err)
		}
		signals = append(signals, sigs...)

	case "linear":
		sigs, err := r.readLinear(acct, since, until)
		if err != nil {
			errs = append(errs, err)
		}
		signals = append(signals, sigs...)
	}

	return signals, errors.Join(errs...)
}

// readConversations reads all Slack/WhatsApp conversations for an account.
func (r *Reader) readConversations(acct account.Account, since, until time.Time) ([]models.Signal, error) {
	convs, err := r.store.ListConversations(acct)
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}

	sigType := models.SignalSlackMessage
	if acct.Platform == "whatsapp" {
		sigType = models.SignalWhatsApp
	}

	var signals []models.Signal
	var errs []error

	for _, conv := range convs {
		if conv == paths.IdentitySubdir {
			continue
		}
		convDir := r.root.AccountFor(acct).Conversation(conv)
		dateFiles, err := listDateFiles(convDir.Path())
		if err != nil {
			errs = append(errs, fmt.Errorf("list date files %s/%s: %w", acct.Display(), conv, err))
			continue
		}
		for _, df := range dateFiles {
			if !dateFileInRange(df, since, until) {
				continue
			}
			lines, err := readLines(df)
			if err != nil {
				errs = append(errs, fmt.Errorf("read %s: %w", df, err))
			}
			for _, line := range lines {
				if line.Type != modelv1.LineMessage || line.Msg == nil {
					continue
				}
				if !inRange(line.Msg.Ts, since, until) {
					continue
				}
				signals = append(signals, models.Signal{
					ID:           line.Msg.ID,
					Type:         sigType,
					Account:      acct,
					Conversation: conv,
					Ts:           line.Msg.Ts,
					Sender:       line.Msg.Sender,
					Text:         line.Msg.Text,
				})
			}
		}
	}

	return signals, errors.Join(errs...)
}

// readGmail reads all Gmail date files for an account.
func (r *Reader) readGmail(acct account.Account, since, until time.Time) ([]models.Signal, error) {
	gmailDir := r.root.AccountFor(acct).Gmail()
	dateFiles, err := listDateFiles(gmailDir.Path())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list gmail date files: %w", err)
	}

	var signals []models.Signal
	var errs []error

	for _, df := range dateFiles {
		if !dateFileInRange(df, since, until) {
			continue
		}
		lines, err := readLines(df)
		if err != nil {
			errs = append(errs, fmt.Errorf("read %s: %w", df, err))
		}
		for _, line := range lines {
			if line.Type != modelv1.LineEmail || line.Email == nil {
				continue
			}
			if !inRange(line.Email.Ts, since, until) {
				continue
			}
			text := line.Email.Subject
			if line.Email.Snippet != "" {
				text += " " + line.Email.Snippet
			}
			signals = append(signals, models.Signal{
				ID:           line.Email.ID,
				Type:         models.SignalEmail,
				Account:      acct,
				Conversation: line.Email.ThreadID,
				ThreadID:     line.Email.ThreadID,
				Ts:           line.Email.Ts,
				Sender:       line.Email.FromName,
				Text:         text,
			})
		}
	}

	return signals, errors.Join(errs...)
}

// readCalendars reads all calendar event files for an account.
func (r *Reader) readCalendars(acct account.Account, since, until time.Time) ([]models.Signal, error) {
	gcalBase := filepath.Join(r.root.AccountFor(acct).Path(), paths.GcalendarSubdir)
	calDirs, err := listSubdirs(gcalBase)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list calendars: %w", err)
	}

	var signals []models.Signal
	var errs []error

	for _, calID := range calDirs {
		calDir := r.root.AccountFor(acct).Calendar(calID)
		dateFiles, err := listDateFiles(calDir.Path())
		if err != nil {
			errs = append(errs, fmt.Errorf("list calendar date files %s: %w", calID, err))
			continue
		}
		for _, df := range dateFiles {
			if !dateFileInRange(df, since, until) {
				continue
			}
			lines, err := readLines(df)
			if err != nil {
				errs = append(errs, fmt.Errorf("read %s: %w", df, err))
			}
			for _, line := range lines {
				if line.Type != modelv1.LineEvent || line.Event == nil {
					continue
				}
				ts := parseEventTime(line.Event)
				if ts.IsZero() || !inRange(ts, since, until) {
					continue
				}
				sender := ""
				if line.Event.Runtime.Creator != nil {
					sender = line.Event.Runtime.Creator.DisplayName
				}
				signals = append(signals, models.Signal{
					ID:           line.Event.Runtime.Id,
					Type:         models.SignalCalendarEvent,
					Account:      acct,
					Conversation: calID,
					Ts:           ts,
					Sender:       sender,
					Text:         line.Event.Runtime.Summary,
				})
			}
		}
	}

	return signals, errors.Join(errs...)
}

// readDriveComments reads all Drive comment files for an account.
func (r *Reader) readDriveComments(acct account.Account, since, until time.Time) ([]models.Signal, error) {
	driveDir := r.root.AccountFor(acct).Drive()
	fileDirs, err := listSubdirs(driveDir.Path())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list drive files: %w", err)
	}

	var signals []models.Signal
	var errs []error

	for _, fileSlug := range fileDirs {
		commentsFile := driveDir.File(fileSlug).CommentsFile()
		lines, err := readLines(commentsFile.Path())
		if err != nil {
			errs = append(errs, fmt.Errorf("read drive comments %s: %w", fileSlug, err))
			continue
		}
		for _, line := range lines {
			if line.Type != modelv1.LineComment || line.Comment == nil {
				continue
			}
			ts, _ := time.Parse(time.RFC3339, line.Comment.Runtime.CreatedTime)
			if ts.IsZero() || !inRange(ts, since, until) {
				continue
			}
			sender := ""
			if line.Comment.Runtime.Author != nil {
				sender = line.Comment.Runtime.Author.DisplayName
			}
			signals = append(signals, models.Signal{
				ID:           line.Comment.Runtime.Id,
				Type:         models.SignalDriveComment,
				Account:      acct,
				Conversation: fileSlug,
				Ts:           ts,
				Sender:       sender,
				Text:         line.Comment.Runtime.Content,
			})
		}
	}

	return signals, errors.Join(errs...)
}

// readLinear reads all Linear issue and comment files for an account.
func (r *Reader) readLinear(acct account.Account, since, until time.Time) ([]models.Signal, error) {
	linearDir := r.root.AccountFor(acct).Linear()
	issueFiles, err := listJSONLFiles(linearDir.IssuesDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list linear issues: %w", err)
	}

	var signals []models.Signal
	var errs []error

	for _, issueFile := range issueFiles {
		identifier := strings.TrimSuffix(filepath.Base(issueFile), paths.FileExt)
		lines, err := readLines(issueFile)
		if err != nil {
			errs = append(errs, fmt.Errorf("read linear issue %s: %w", identifier, err))
			continue
		}
		for _, line := range lines {
			switch {
			case line.Type == modelv1.LineLinearIssue && line.Issue != nil:
				ts, _ := time.Parse(time.RFC3339, line.Issue.Runtime.UpdatedAt)
				if ts.IsZero() || !inRange(ts, since, until) {
					continue
				}
				title, _ := line.Issue.Serialized["title"].(string)
				signals = append(signals, models.Signal{
					ID:           line.Issue.Runtime.ID,
					Type:         models.SignalLinearIssue,
					Account:      acct,
					Conversation: identifier,
					Ts:           ts,
					Sender:       "", // Linear issues don't have a single sender
					Text:         line.Issue.Runtime.Identifier + " " + title,
				})

			case line.Type == modelv1.LineLinearComment && line.LinearComment != nil:
				ts, _ := time.Parse(time.RFC3339, line.LinearComment.Runtime.CreatedAt)
				if ts.IsZero() || !inRange(ts, since, until) {
					continue
				}
				body, _ := line.LinearComment.Serialized["body"].(string)
				signals = append(signals, models.Signal{
					ID:           line.LinearComment.Runtime.ID,
					Type:         models.SignalLinearComment,
					Account:      acct,
					Conversation: identifier,
					Ts:           ts,
					Text:         body,
				})
			}
		}
	}

	return signals, errors.Join(errs...)
}

// parseEventTime extracts a time.Time from a calendar event's start time.
func parseEventTime(event *modelv1.CalendarEvent) time.Time {
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

// inRange reports whether t falls within [since, until].
func inRange(t, since, until time.Time) bool {
	return !t.Before(since) && !t.After(until)
}

// dateFileInRange checks whether a date file's date might overlap with the
// given time range. The date file covers an entire day, so we check whether
// the day intersects the [since, until] window.
func dateFileInRange(path string, since, until time.Time) bool {
	base := filepath.Base(path)
	dateStr := strings.TrimSuffix(base, paths.FileExt)
	d, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return true // can't parse date, include the file to be safe
	}
	dayEnd := d.Add(24*time.Hour - time.Nanosecond)
	return !dayEnd.Before(since) && !d.After(until)
}

// listDateFiles returns sorted paths of YYYY-MM-DD.jsonl files in a directory.
func listDateFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && paths.IsDateFile(e.Name()) {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(files)
	return files, nil
}

// listSubdirs returns the names of non-hidden subdirectories in a directory.
func listSubdirs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			dirs = append(dirs, e.Name())
		}
	}
	return dirs, nil
}

// readLines reads all JSONL lines from a file, using a 2MB buffer to handle
// large email HTML bodies. Returns successfully parsed lines and any errors.
func readLines(path string) ([]modelv1.Line, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 2*1024*1024), 2*1024*1024) // 2MB buffer

	var lines []modelv1.Line
	var errs []error
	for scanner.Scan() {
		text := scanner.Text()
		if text == "" {
			continue
		}
		l, err := modelv1.Parse(text)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		lines = append(lines, l)
	}
	if err := scanner.Err(); err != nil {
		errs = append(errs, fmt.Errorf("scan %s: %w", path, err))
	}
	return lines, errors.Join(errs...)
}

// listJSONLFiles returns sorted paths of *.jsonl files in a directory.
func listJSONLFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), paths.FileExt) {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(files)
	return files, nil
}
