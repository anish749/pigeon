package poller

import (
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/gosimple/slug"

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
func PollDrive(dataDir string, cursors *gwsstore.Cursors) error {
	// Seed the page token if we don't have one yet.
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
			if err := handleDoc(dataDir, ch); err != nil {
				errs = append(errs, fmt.Errorf("handle doc %s: %w", ch.FileID, err))
			}
		case mimeSheet:
			if err := handleSheet(dataDir, ch); err != nil {
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

func handleDoc(dataDir string, ch drive.Change) error {
	doc, err := drive.GetDocument(ch.FileID)
	if err != nil {
		return fmt.Errorf("get document: %w", err)
	}

	docSlug := slug.Make(doc.Title)
	docDir := filepath.Join(dataDir, "gdrive", docSlug)

	tabs := doc.AllTabs()
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

	// Fetch and store comments.
	comments, replies, err := drive.ListComments(ch.FileID)
	if err != nil {
		errs = append(errs, fmt.Errorf("list comments: %w", err))
	} else {
		commentsPath := filepath.Join(docDir, "comments.jsonl")
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
	}

	// Save metadata.
	meta := &model.DocMeta{
		FileID:       ch.FileID,
		MimeType:     ch.File.MimeType,
		Title:        doc.Title,
		ModifiedTime: time.Now().UTC().Format(time.RFC3339),
		SyncedAt:     time.Now().UTC().Format(time.RFC3339),
		Tabs:         tabMetas,
	}
	metaPath := filepath.Join(docDir, "meta.json")
	if err := gwsstore.SaveMeta(metaPath, meta); err != nil {
		errs = append(errs, fmt.Errorf("save meta: %w", err))
	}

	return errors.Join(errs...)
}

func handleSheet(dataDir string, ch drive.Change) error {
	sheetNames, err := drive.GetSheetNames(ch.FileID)
	if err != nil {
		return fmt.Errorf("get sheet names: %w", err)
	}

	sheetSlug := slug.Make(ch.File.Name)
	sheetDir := filepath.Join(dataDir, "gdrive", sheetSlug)

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

	// Fetch and store comments.
	comments, replies, err := drive.ListComments(ch.FileID)
	if err != nil {
		errs = append(errs, fmt.Errorf("list comments: %w", err))
	} else {
		commentsPath := filepath.Join(sheetDir, "comments.jsonl")
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
	}

	// Save metadata.
	meta := &model.DocMeta{
		FileID:       ch.FileID,
		MimeType:     ch.File.MimeType,
		Title:        ch.File.Name,
		ModifiedTime: time.Now().UTC().Format(time.RFC3339),
		SyncedAt:     time.Now().UTC().Format(time.RFC3339),
		Sheets:       sheetNames,
	}
	metaPath := filepath.Join(sheetDir, "meta.json")
	if err := gwsstore.SaveMeta(metaPath, meta); err != nil {
		errs = append(errs, fmt.Errorf("save meta: %w", err))
	}

	return errors.Join(errs...)
}
