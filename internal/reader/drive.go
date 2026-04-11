package reader

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store/modelv1"
)

// DriveResult holds the output of reading a Drive document or sheet.
type DriveResult struct {
	Title    string                 // original document title from metadata
	MimeType string                 // doc or sheet
	Tabs     []DriveTab             // markdown tabs (docs) or CSV sheets
	Comments []modelv1.DriveComment // deduplicated comments
}

// DriveTab holds one tab or sheet within a Drive file.
type DriveTab struct {
	Name    string // tab/sheet name (filename stem)
	Content string // markdown or CSV content
}

// DriveMeta is the parsed drive-meta-YYYY-MM-DD.json file.
type DriveMeta struct {
	FileID       string `json:"fileId"`
	MimeType     string `json:"mimeType"`
	Title        string `json:"title"`
	ModifiedTime string `json:"modifiedTime"`
	SyncedAt     string `json:"syncedAt"`
	Tabs         []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"tabs,omitempty"`
	Sheets []string `json:"sheets,omitempty"`
}

// ReadDrive reads a Drive file directory (doc or sheet), returning its
// content tabs and comments.
//
// Algorithm (from read-protocol.md):
//
// Docs:
//  1. Read {TabName}.md files and present as markdown.
//  2. Parse comments.jsonl, deduplicate by id.
//
// Sheets:
//  1. Read {SheetName}.csv files and present as tables.
//  2. Comments: same as Docs.
func ReadDrive(dir paths.DriveFileDir) (*DriveResult, error) {
	// Read metadata to get title and type.
	meta, err := readDriveMeta(dir.Path())
	if err != nil {
		return nil, fmt.Errorf("read drive metadata in %s: %w", dir.Path(), err)
	}

	result := &DriveResult{
		Title:    meta.Title,
		MimeType: meta.MimeType,
	}

	// Read content files (markdown or CSV).
	isDoc := strings.Contains(meta.MimeType, "document")
	ext := paths.MarkdownExt
	if !isDoc {
		ext = paths.CSVExt
	}

	entries, err := os.ReadDir(dir.Path())
	if err != nil {
		return nil, fmt.Errorf("read drive dir %s: %w", dir.Path(), err)
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if filepath.Ext(e.Name()) != ext {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir.Path(), e.Name()))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", e.Name(), err)
		}
		result.Tabs = append(result.Tabs, DriveTab{
			Name:    strings.TrimSuffix(e.Name(), ext),
			Content: string(content),
		})
	}

	// Read and deduplicate comments.
	comments, err := readDriveComments(dir.CommentsFile().Path())
	if err != nil {
		return nil, fmt.Errorf("read drive comments: %w", err)
	}
	result.Comments = comments

	return result, nil
}

// FindDriveFile fuzzy-matches a selector against drive file directory names
// within a DriveDir. Returns the matched DriveFileDir or an error with
// candidates if ambiguous or no match.
func FindDriveFile(driveDir paths.DriveDir, selector string) (paths.DriveFileDir, error) {
	entries, err := os.ReadDir(driveDir.Path())
	if err != nil {
		if os.IsNotExist(err) {
			return paths.DriveFileDir{}, fmt.Errorf("no drive data for this account")
		}
		return paths.DriveFileDir{}, fmt.Errorf("read drive dir: %w", err)
	}

	q := strings.ToLower(selector)
	var matches []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		// Try matching against directory name.
		if strings.Contains(strings.ToLower(name), q) {
			matches = append(matches, name)
			continue
		}
		// Also try matching against the title in metadata.
		meta, err := readDriveMeta(filepath.Join(driveDir.Path(), name))
		if err != nil {
			// No metadata or unreadable — skip this entry for title matching
			// but don't fail the whole search. The directory was already tried
			// by slug above.
			continue
		}
		if strings.Contains(strings.ToLower(meta.Title), q) {
			matches = append(matches, name)
		}
	}

	switch len(matches) {
	case 0:
		return paths.DriveFileDir{}, fmt.Errorf("no drive document matching %q", selector)
	case 1:
		return driveDir.File(matches[0]), nil
	default:
		return paths.DriveFileDir{}, fmt.Errorf("ambiguous drive document %q — matches: %s", selector, strings.Join(matches, ", "))
	}
}

// readDriveMeta finds and parses the drive-meta-YYYY-MM-DD.json file in a
// drive file directory.
func readDriveMeta(dir string) (*DriveMeta, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "drive-meta-") && strings.HasSuffix(e.Name(), ".json") {
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				return nil, fmt.Errorf("read %s: %w", e.Name(), err)
			}
			var meta DriveMeta
			if err := json.Unmarshal(data, &meta); err != nil {
				return nil, fmt.Errorf("parse %s: %w", e.Name(), err)
			}
			return &meta, nil
		}
	}
	return nil, fmt.Errorf("no drive-meta file found in %s", dir)
}

// readDriveComments parses and deduplicates comments from a comments.jsonl
// file. Returns nil, nil when the file does not exist (no comments is a
// normal state for files that have never been commented on).
func readDriveComments(path string) ([]modelv1.DriveComment, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var comments []modelv1.DriveComment
	var errs []error
	for _, rawLine := range splitLines(data) {
		line, err := modelv1.Parse(rawLine)
		if err != nil {
			errs = append(errs, fmt.Errorf("parse comment line: %w", err))
			continue
		}
		if line.Type == modelv1.LineComment && line.Comment != nil {
			comments = append(comments, *line.Comment)
		}
	}

	// Dedup by ID (keep last occurrence).
	return dedupComments(comments), errors.Join(errs...)
}

// dedupComments deduplicates comments by ID, keeping the last occurrence.
func dedupComments(comments []modelv1.DriveComment) []modelv1.DriveComment {
	lastIndex := make(map[string]int, len(comments))
	for i, c := range comments {
		lastIndex[c.Runtime.Id] = i
	}
	var result []modelv1.DriveComment
	for i, c := range comments {
		if lastIndex[c.Runtime.Id] == i {
			result = append(result, c)
		}
	}
	return result
}
