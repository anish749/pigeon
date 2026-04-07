package poller

import (
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/gosimple/slug"

	"github.com/anish749/pigeon/internal/gws"
	"github.com/anish749/pigeon/internal/gws/drive"
	"github.com/anish749/pigeon/internal/gws/drive/converter"
	"github.com/anish749/pigeon/internal/gws/gwsstore"
	"github.com/anish749/pigeon/internal/gws/model"
)

const (
	mimeDoc   = "application/vnd.google-apps.document"
	mimeSheet = "application/vnd.google-apps.spreadsheet"
)

// PollDrive polls for Drive changes and processes Docs, Sheets, and comments.
func PollDrive(accountDir string, cursors *gwsstore.Cursors) error {
	if cursors.Drive.PageToken == "" {
		slog.Info("seeding drive page token")
		token, err := drive.SeedPageToken()
		if err != nil {
			return fmt.Errorf("seed drive page token: %w", err)
		}
		cursors.Drive.PageToken = token
		slog.Info("seeded drive page token", "token", token)
		return nil
	}

	changes, newToken, err := drive.ListChanges(cursors.Drive.PageToken)
	if err != nil {
		return fmt.Errorf("poll drive changes: %w", err)
	}

	var errs []error
	for _, ch := range changes {
		if ch.Removed {
			slog.Debug("drive file removed", "fileId", ch.FileID)
			continue
		}

		switch ch.File.MimeType {
		case mimeDoc:
			if err := handleDoc(accountDir, ch); err != nil {
				errs = append(errs, fmt.Errorf("handle doc %s: %w", ch.FileID, err))
			}
		case mimeSheet:
			if err := handleSheet(accountDir, ch); err != nil {
				errs = append(errs, fmt.Errorf("handle sheet %s: %w", ch.FileID, err))
			}
		default:
			slog.Debug("skipping unsupported mime type", "fileId", ch.FileID, "mimeType", ch.File.MimeType)
		}
	}

	if len(changes) > 0 {
		slog.Info("polled drive", "changes", len(changes))
	}

	cursors.Drive.PageToken = newToken
	return errors.Join(errs...)
}

func handleDoc(accountDir string, ch drive.Change) error {
	doc, err := drive.GetDocument(ch.FileID)
	if err != nil {
		if gws.IsNotFound(err) || gws.IsStatusCode(err, 403) {
			slog.Warn("drive doc inaccessible, skipping", "fileId", ch.FileID, "error", err)
			return nil
		}
		return fmt.Errorf("get document: %w", err)
	}

	docDir := filepath.Join(accountDir, "gdrive", driveSlug(doc.Title, ch.FileID))

	tabs, err := doc.AllTabs()
	if err != nil {
		return fmt.Errorf("flatten tabs: %w", err)
	}

	md := converter.NewMarkdownConverter()
	var tabMetas []model.TabMeta
	var errs []error

	for _, tab := range tabs {
		content := md.Convert(tab)
		tabFile := filepath.Join(docDir, tab.Title+".md")
		if err := gwsstore.WriteContent(tabFile, []byte(content)); err != nil {
			errs = append(errs, fmt.Errorf("write tab %s: %w", tab.Title, err))
		}
		tabMetas = append(tabMetas, model.TabMeta{ID: tab.TabID, Title: tab.Title})
	}

	if err := storeComments(docDir, ch.FileID); err != nil {
		errs = append(errs, err)
	}

	meta := &model.DocMeta{
		FileID:       ch.FileID,
		MimeType:     ch.File.MimeType,
		Title:        doc.Title,
		ModifiedTime: ch.File.ModifiedTime,
		SyncedAt:     time.Now().UTC().Format(time.RFC3339),
		Tabs:         tabMetas,
	}
	if err := gwsstore.SaveMeta(filepath.Join(docDir, "meta.json"), meta); err != nil {
		errs = append(errs, fmt.Errorf("save meta: %w", err))
	}

	return errors.Join(errs...)
}

func handleSheet(accountDir string, ch drive.Change) error {
	sheetNames, err := drive.GetSheetNames(ch.FileID)
	if err != nil {
		if gws.IsNotFound(err) || gws.IsStatusCode(err, 403) {
			slog.Warn("drive sheet inaccessible, skipping", "fileId", ch.FileID, "error", err)
			return nil
		}
		return fmt.Errorf("get sheet names: %w", err)
	}

	sheetDir := filepath.Join(accountDir, "gdrive", driveSlug(ch.File.Name, ch.FileID))

	var errs []error

	for _, name := range sheetNames {
		values, err := drive.ReadSheetValues(ch.FileID, name)
		if err != nil {
			errs = append(errs, fmt.Errorf("read sheet %s: %w", name, err))
			continue
		}
		csvData, err := converter.ToCSV(values)
		if err != nil {
			errs = append(errs, fmt.Errorf("convert sheet %s to csv: %w", name, err))
			continue
		}
		csvFile := filepath.Join(sheetDir, name+".csv")
		if err := gwsstore.WriteContent(csvFile, csvData); err != nil {
			errs = append(errs, fmt.Errorf("write sheet %s: %w", name, err))
		}
	}

	if err := storeComments(sheetDir, ch.FileID); err != nil {
		errs = append(errs, err)
	}

	meta := &model.DocMeta{
		FileID:       ch.FileID,
		MimeType:     ch.File.MimeType,
		Title:        ch.File.Name,
		ModifiedTime: ch.File.ModifiedTime,
		SyncedAt:     time.Now().UTC().Format(time.RFC3339),
		Sheets:       sheetNames,
	}
	if err := gwsstore.SaveMeta(filepath.Join(sheetDir, "meta.json"), meta); err != nil {
		errs = append(errs, fmt.Errorf("save meta: %w", err))
	}

	return errors.Join(errs...)
}

// driveSlug creates a directory name for a Drive file. Uses the slugified
// title with a short file ID suffix to prevent collisions (two docs titled
// "Meeting Notes" get different directories). Falls back to the file ID
// alone if the title is empty.
func driveSlug(title, fileID string) string {
	s := slug.Make(title)
	suffix := fileID
	if len(suffix) > 8 {
		suffix = suffix[:8]
	}
	if s == "" {
		return suffix
	}
	return s + "-" + suffix
}

// storeComments fetches comments and replies for a Drive file and appends
// them to comments.jsonl in the given directory.
func storeComments(dir, fileID string) error {
	comments, replies, err := drive.ListComments(fileID)
	if err != nil {
		return fmt.Errorf("list comments for %s: %w", fileID, err)
	}

	commentsPath := filepath.Join(dir, "comments.jsonl")
	var errs []error
	for _, c := range comments {
		line := model.Line{Type: "comment", Comment: &c}
		if err := gwsstore.AppendLine(commentsPath, line); err != nil {
			errs = append(errs, fmt.Errorf("append comment %s: %w", c.ID, err))
		}
	}
	for _, r := range replies {
		line := model.Line{Type: "reply", Reply: &r}
		if err := gwsstore.AppendLine(commentsPath, line); err != nil {
			errs = append(errs, fmt.Errorf("append reply %s: %w", r.ID, err))
		}
	}
	return errors.Join(errs...)
}
