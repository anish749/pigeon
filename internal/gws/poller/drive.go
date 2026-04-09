package poller

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gosimple/slug"

	"github.com/anish749/pigeon/internal/gws"
	"github.com/anish749/pigeon/internal/gws/drive"
	"github.com/anish749/pigeon/internal/gws/drive/converter"
	"github.com/anish749/pigeon/internal/gws/gwsstore"
	"github.com/anish749/pigeon/internal/gws/model"
	"github.com/anish749/pigeon/internal/paths"
)

const (
	mimeDoc   = "application/vnd.google-apps.document"
	mimeSheet = "application/vnd.google-apps.spreadsheet"
)

// PollDrive polls for Drive changes and processes Docs, Sheets, and comments.
// Returns the number of changes observed plus any error. On initial seed
// it returns the backfilled file count.
func PollDrive(account paths.AccountDir, cursors *gwsstore.Cursors) (int, error) {
	if cursors.Drive.PageToken == "" {
		return seedDrive(account, cursors)
	}

	changes, newToken, err := drive.ListChanges(cursors.Drive.PageToken)
	if err != nil {
		return 0, fmt.Errorf("poll drive changes: %w", err)
	}

	var errs []error
	for _, ch := range changes {
		if ch.Removed {
			slog.Warn("drive file removed", "fileId", ch.FileID)
			continue
		}

		switch ch.File.MimeType {
		case mimeDoc:
			if err := handleDoc(account, ch); err != nil {
				errs = append(errs, fmt.Errorf("handle doc %s: %w", ch.FileID, err))
			}
		case mimeSheet:
			if err := handleSheet(account, ch); err != nil {
				errs = append(errs, fmt.Errorf("handle sheet %s: %w", ch.FileID, err))
			}
		default:
			slog.Warn("skipping unsupported mime type", "fileId", ch.FileID, "mimeType", ch.File.MimeType)
		}
	}

	if len(changes) > 0 {
		slog.Info("polled drive", "changes", len(changes))
	}

	cursors.Drive.PageToken = newToken
	return len(changes), errors.Join(errs...)
}

// seedDrive acquires the changes cursor, backfills existing Docs and Sheets
// modified within BackfillDays, then saves the cursor. The cursor is acquired
// BEFORE backfill so that changes made during the (potentially slow) backfill
// are captured by the first incremental poll.
func seedDrive(account paths.AccountDir, cursors *gwsstore.Cursors) (int, error) {
	slog.Info("seeding drive with backfill")

	// Get the changes cursor first — backfill can take minutes.
	token, err := drive.SeedPageToken()
	if err != nil {
		return 0, fmt.Errorf("seed drive page token: %w", err)
	}

	timeMin := time.Now().UTC().AddDate(0, 0, -gws.BackfillDays).Format(time.RFC3339)
	files, err := drive.ListFiles(timeMin)
	if err != nil {
		return 0, fmt.Errorf("backfill drive: %w", err)
	}

	var errs []error
	for _, ch := range files {
		switch ch.File.MimeType {
		case mimeDoc:
			if err := handleDoc(account, ch); err != nil {
				errs = append(errs, fmt.Errorf("backfill doc %s: %w", ch.FileID, err))
			}
		case mimeSheet:
			if err := handleSheet(account, ch); err != nil {
				errs = append(errs, fmt.Errorf("backfill sheet %s: %w", ch.FileID, err))
			}
		}
	}

	cursors.Drive.PageToken = token
	slog.Info("seeded drive with backfill", "files", len(files), "token", token)
	return len(files), errors.Join(errs...)
}

func handleDoc(account paths.AccountDir, ch drive.Change) error {
	doc, err := drive.GetDocument(ch.FileID)
	if err != nil {
		if gws.IsNotFound(err) || gws.IsStatusCode(err, 403) {
			slog.Warn("drive doc inaccessible, skipping", "fileId", ch.FileID, "error", err)
			return nil
		}
		return fmt.Errorf("get document: %w", err)
	}

	fileDir := account.Drive().File(driveSlug(doc.Title, ch.FileID))

	tabs, err := doc.AllTabs()
	if err != nil {
		return fmt.Errorf("flatten tabs: %w", err)
	}

	md := converter.NewMarkdownConverter()
	var tabMetas []model.TabMeta
	var errs []error

	for _, tab := range tabs {
		result := md.Convert(tab)
		if err := gwsstore.WriteContent(fileDir.TabFile(tab.Title), []byte(result.Markdown)); err != nil {
			errs = append(errs, fmt.Errorf("write tab %s: %w", tab.Title, err))
		}
		tabMetas = append(tabMetas, model.TabMeta{ID: tab.TabID, Title: tab.Title})

		// Download inline images.
		for _, img := range result.Images {
			if err := downloadImage(fileDir.AttachmentFile(img.Filename), img.ImageURI); err != nil {
				errs = append(errs, fmt.Errorf("download image %s: %w", img.ObjectID, err))
			}
		}
	}

	if err := storeComments(fileDir, ch.FileID); err != nil {
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
	if err := gwsstore.SaveMeta(fileDir.MetaFile(), meta); err != nil {
		errs = append(errs, fmt.Errorf("save meta: %w", err))
	}

	return errors.Join(errs...)
}

func handleSheet(account paths.AccountDir, ch drive.Change) error {
	sheetNames, err := drive.GetSheetNames(ch.FileID)
	if err != nil {
		if gws.IsNotFound(err) || gws.IsStatusCode(err, 403) {
			slog.Warn("drive sheet inaccessible, skipping", "fileId", ch.FileID, "error", err)
			return nil
		}
		return fmt.Errorf("get sheet names: %w", err)
	}

	fileDir := account.Drive().File(driveSlug(ch.File.Name, ch.FileID))

	var errs []error

	for _, name := range sheetNames {
		// Values.
		values, err := drive.ReadSheetValues(ch.FileID, name)
		if err != nil {
			errs = append(errs, fmt.Errorf("read sheet %s values: %w", name, err))
			continue
		}
		csvData, err := converter.ToCSV(values)
		if err != nil {
			errs = append(errs, fmt.Errorf("convert sheet %s to csv: %w", name, err))
			continue
		}
		if err := gwsstore.WriteContent(fileDir.SheetFile(name), csvData); err != nil {
			errs = append(errs, fmt.Errorf("write sheet %s csv: %w", name, err))
		}

		// Formulas.
		formulas, err := drive.ReadSheetFormulas(ch.FileID, name)
		if err != nil {
			errs = append(errs, fmt.Errorf("read sheet %s formulas: %w", name, err))
			continue
		}
		formulaCSV, err := converter.ToCSV(formulas)
		if err != nil {
			errs = append(errs, fmt.Errorf("convert sheet %s formulas to csv: %w", name, err))
			continue
		}
		if err := gwsstore.WriteContent(fileDir.FormulaFile(name), formulaCSV); err != nil {
			errs = append(errs, fmt.Errorf("write sheet %s formulas csv: %w", name, err))
		}
	}

	if err := storeComments(fileDir, ch.FileID); err != nil {
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
	if err := gwsstore.SaveMeta(fileDir.MetaFile(), meta); err != nil {
		errs = append(errs, fmt.Errorf("save meta: %w", err))
	}

	return errors.Join(errs...)
}

// downloadImage fetches an image from a URL and writes it to path.
// Creates parent directories if needed. Skips if the file already exists.
//
// The Docs API contentUri is a signed lh7-rt.googleusercontent.com URL
// with a ?key= parameter — publicly accessible without auth headers.
// Validated against a real doc: unauthenticated http.Get returns the
// image bytes (PNG, 90KB) with HTTP 200. The URI is short-lived but
// we download immediately during the poll cycle.
func downloadImage(af paths.AttachmentFile, uri string) error {
	path := af.Path()
	if _, err := os.Stat(path); err == nil {
		return nil // already downloaded
	}

	resp, err := http.Get(uri)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", uri, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch %s: HTTP %d", uri, resp.StatusCode)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create dir for %s: %w", path, err)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// driveSlug creates a directory name for a Drive file. Uses the slugified
// title with the full file ID to prevent collisions. Falls back to the
// file ID alone if the title is empty.
func driveSlug(title, fileID string) string {
	s := slug.Make(title)
	if s == "" {
		return fileID
	}
	return s + "-" + fileID
}

// storeComments fetches all comments and replies for a Drive file and writes
// them to comments.jsonl, replacing the previous contents. The Drive Comments
// API has no incremental sync — every call returns the full snapshot. Overwrite
// ensures deleted comments disappear and resolved status stays current.
func storeComments(fileDir paths.DriveFileDir, fileID string) error {
	comments, replies, err := drive.ListComments(fileID)
	if err != nil {
		return fmt.Errorf("list comments for %s: %w", fileID, err)
	}

	var lines []model.Line
	for _, c := range comments {
		lines = append(lines, model.Line{Type: "comment", Comment: &c})
	}
	for _, r := range replies {
		lines = append(lines, model.Line{Type: "reply", Reply: &r})
	}

	if err := gwsstore.WriteLines(fileDir.CommentsFile(), lines); err != nil {
		return fmt.Errorf("write comments for %s: %w", fileID, err)
	}
	return nil
}
