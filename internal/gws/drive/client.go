// Package drive wraps the gws CLI for Google Drive, Docs, and Sheets API calls.
package drive

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/anish749/pigeon/internal/gws"
	"github.com/anish749/pigeon/internal/gws/model"
)

// --- Changes API ---

type changesResponse struct {
	Changes       []Change `json:"changes"`
	NewStartToken string   `json:"newStartPageToken"`
	NextPageToken string   `json:"nextPageToken"`
}

// Change represents a single file change from the Drive Changes API.
type Change struct {
	FileID  string `json:"fileId"`
	Removed bool   `json:"removed"`
	File    File   `json:"file"`
}

// File holds metadata for a changed file.
type File struct {
	Name         string `json:"name"`
	MimeType     string `json:"mimeType"`
	ModifiedTime string `json:"modifiedTime"`
}

// ListChanges fetches all changes since pageToken. Paginates through all pages.
// Returns the changes and the new pageToken for the next poll.
func ListChanges(pageToken string) ([]Change, string, error) {
	var allChanges []Change
	var newPageToken string

	currentToken := pageToken
	for {
		params := gws.ParamsJSON(map[string]string{
			"pageToken": currentToken,
			"fields":    "changes(fileId,removed,file(name,mimeType,modifiedTime)),newStartPageToken,nextPageToken",
		})

		var resp changesResponse
		if err := gws.RunParsed(&resp, "drive", "changes", "list", "--params", params); err != nil {
			return nil, "", fmt.Errorf("list drive changes: %w", err)
		}

		allChanges = append(allChanges, resp.Changes...)

		if resp.NextPageToken != "" {
			currentToken = resp.NextPageToken
			continue
		}

		newPageToken = resp.NewStartToken
		break
	}

	return allChanges, newPageToken, nil
}

// seedPageTokenResponse is the response from drive.changes.getStartPageToken.
type seedPageTokenResponse struct {
	StartPageToken string `json:"startPageToken"`
}

// SeedPageToken gets the starting page token for future change polling.
func SeedPageToken() (string, error) {
	var resp seedPageTokenResponse
	if err := gws.RunParsed(&resp, "drive", "changes", "getStartPageToken"); err != nil {
		return "", fmt.Errorf("seed drive page token: %w", err)
	}
	if resp.StartPageToken == "" {
		return "", fmt.Errorf("seed drive page token: empty startPageToken in response")
	}
	return resp.StartPageToken, nil
}

// ListFiles enumerates Docs and Sheets modified after timeMin. Returns them
// as Change structs so callers can use the same handleDoc/handleSheet pipeline.
// Results are ordered by modifiedTime descending (most recent first).
func ListFiles(timeMin string) ([]Change, error) {
	q := fmt.Sprintf(
		"modifiedTime > '%s' and (mimeType = 'application/vnd.google-apps.document' or mimeType = 'application/vnd.google-apps.spreadsheet') and trashed = false",
		timeMin,
	)
	params := map[string]string{
		"q":       q,
		"orderBy": "modifiedTime desc",
		"fields":  "files(id,name,mimeType,modifiedTime),nextPageToken",
	}

	var allChanges []Change
	for {
		var resp filesListResponse
		if err := gws.RunParsed(&resp, "drive", "files", "list", "--params", gws.ParamsJSON(params)); err != nil {
			return nil, fmt.Errorf("list drive files: %w", err)
		}

		for _, f := range resp.Files {
			allChanges = append(allChanges, Change{
				FileID: f.ID,
				File: File{
					Name:         f.Name,
					MimeType:     f.MimeType,
					ModifiedTime: f.ModifiedTime,
				},
			})
		}

		if resp.NextPageToken != "" {
			params["pageToken"] = resp.NextPageToken
			continue
		}
		break
	}

	return allChanges, nil
}

type filesListResponse struct {
	Files         []filesListFile `json:"files"`
	NextPageToken string          `json:"nextPageToken"`
}

type filesListFile struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	MimeType     string `json:"mimeType"`
	ModifiedTime string `json:"modifiedTime"`
}

// --- Docs API ---

// GetDocument fetches a Google Doc with all tab content.
func GetDocument(docID string) (*model.Document, error) {
	// includeTabsContent is a boolean, so we use json.Marshal directly
	// instead of ParamsJSON (which only handles map[string]string).
	params := map[string]any{"documentId": docID, "includeTabsContent": true}
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("marshal doc params: %w", err)
	}

	var doc model.Document
	if err := gws.RunParsed(&doc, "docs", "documents", "get", "--params", string(paramsJSON)); err != nil {
		return nil, fmt.Errorf("get document %s: %w", docID, err)
	}
	return &doc, nil
}

// --- Sheets API ---

type sheetsMetaResponse struct {
	Sheets []sheetMeta `json:"sheets"`
}

type sheetMeta struct {
	Properties sheetProperties `json:"properties"`
}

type sheetProperties struct {
	SheetID int    `json:"sheetId"`
	Title   string `json:"title"`
	Index   int    `json:"index"`
}

// GetSheetNames fetches the sheet tab names for a spreadsheet.
func GetSheetNames(spreadsheetID string) ([]string, error) {
	params := gws.ParamsJSON(map[string]string{
		"spreadsheetId": spreadsheetID,
		"fields":        "sheets.properties(sheetId,title,index)",
	})

	var resp sheetsMetaResponse
	if err := gws.RunParsed(&resp, "sheets", "spreadsheets", "get", "--params", params); err != nil {
		return nil, fmt.Errorf("get sheet names %s: %w", spreadsheetID, err)
	}

	var names []string
	for _, s := range resp.Sheets {
		names = append(names, s.Properties.Title)
	}
	return names, nil
}

type sheetValuesResponse struct {
	Values [][]string `json:"values"`
}

// ReadSheetValues fetches cell values for a specific sheet range.
func ReadSheetValues(spreadsheetID, sheetName string) ([][]string, error) {
	return readSheetRange(spreadsheetID, sheetName, "FORMATTED_VALUE")
}

// ReadSheetFormulas fetches formulas for a specific sheet range.
// Cells with formulas return the formula string (e.g. "=SUM(A1:A10)");
// cells without formulas return the computed value.
func ReadSheetFormulas(spreadsheetID, sheetName string) ([][]string, error) {
	return readSheetRange(spreadsheetID, sheetName, "FORMULA")
}

func readSheetRange(spreadsheetID, sheetName, renderOption string) ([][]string, error) {
	params := gws.ParamsJSON(map[string]string{
		"spreadsheetId":     spreadsheetID,
		"range":             sheetName,
		"valueRenderOption": renderOption,
	})

	var resp sheetValuesResponse
	if err := gws.RunParsed(&resp, "sheets", "spreadsheets", "values", "get", "--params", params); err != nil {
		return nil, fmt.Errorf("read sheet %s %s/%s: %w", renderOption, spreadsheetID, sheetName, err)
	}
	return resp.Values, nil
}

// --- Comments API ---

type commentsResponse struct {
	Comments      []driveComment `json:"comments"`
	NextPageToken string         `json:"nextPageToken"`
}

type driveComment struct {
	ID                string         `json:"id"`
	Author            driveUser      `json:"author"`
	Content           string         `json:"content"`
	QuotedFileContent *quotedContent `json:"quotedFileContent"`
	Resolved          bool           `json:"resolved"`
	CreatedTime       string         `json:"createdTime"`
	ModifiedTime      string         `json:"modifiedTime"`
	Replies           []driveReply   `json:"replies"`
}

type driveUser struct {
	DisplayName string `json:"displayName"`
}

type quotedContent struct {
	Value string `json:"value"`
}

type driveReply struct {
	ID           string    `json:"id"`
	Author       driveUser `json:"author"`
	Content      string    `json:"content"`
	CreatedTime  string    `json:"createdTime"`
	ModifiedTime string    `json:"modifiedTime"`
	Action       string    `json:"action"`
}

// ListComments fetches all comments on a file, including replies. Paginates
// through all pages. Returns comment lines and reply lines.
func ListComments(fileID string) ([]model.CommentLine, []model.ReplyLine, error) {
	var comments []model.CommentLine
	var replies []model.ReplyLine

	params := map[string]string{
		"fileId": fileID,
		"fields": "comments(id,author,content,quotedFileContent,resolved,createdTime,modifiedTime,replies(id,author,content,createdTime,modifiedTime,action)),nextPageToken",
	}

	for {
		var resp commentsResponse
		if err := gws.RunParsed(&resp, "drive", "comments", "list", "--params", gws.ParamsJSON(params)); err != nil {
			return nil, nil, fmt.Errorf("list comments for %s: %w", fileID, err)
		}

		var errs []error
		for _, dc := range resp.Comments {
			cl, err := toCommentLine(dc)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			comments = append(comments, cl)

			for _, dr := range dc.Replies {
				rl, err := toReplyLine(dc.ID, dr)
				if err != nil {
					errs = append(errs, err)
					continue
				}
				replies = append(replies, rl)
			}
		}
		if err := errors.Join(errs...); err != nil {
			return comments, replies, err
		}

		if resp.NextPageToken != "" {
			params["pageToken"] = resp.NextPageToken
			continue
		}
		break
	}

	return comments, replies, nil
}

func toCommentLine(dc driveComment) (model.CommentLine, error) {
	ts, err := time.Parse(time.RFC3339, dc.CreatedTime)
	if err != nil {
		return model.CommentLine{}, fmt.Errorf("parse comment %s time %q: %w", dc.ID, dc.CreatedTime, err)
	}
	var anchor string
	if dc.QuotedFileContent != nil {
		anchor = dc.QuotedFileContent.Value
	}
	return model.CommentLine{
		ID:       dc.ID,
		Ts:       ts,
		Author:   dc.Author.DisplayName,
		Content:  dc.Content,
		Anchor:   anchor,
		Resolved: dc.Resolved,
	}, nil
}

func toReplyLine(commentID string, dr driveReply) (model.ReplyLine, error) {
	ts, err := time.Parse(time.RFC3339, dr.CreatedTime)
	if err != nil {
		return model.ReplyLine{}, fmt.Errorf("parse reply %s time %q: %w", dr.ID, dr.CreatedTime, err)
	}
	return model.ReplyLine{
		ID:        dr.ID,
		CommentID: commentID,
		Ts:        ts,
		Author:    dr.Author.DisplayName,
		Content:   dr.Content,
		Action:    dr.Action,
	}, nil
}
