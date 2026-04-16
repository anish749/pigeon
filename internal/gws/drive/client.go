// Package drive wraps the gws CLI for Google Drive, Docs, and Sheets API calls.
package drive

import (
	"encoding/json"
	"fmt"

	"github.com/anish749/pigeon/internal/gws"
	"github.com/anish749/pigeon/internal/store/modelv1"
	drive "google.golang.org/api/drive/v3"
)

// Client wraps a gws.Client for Drive, Docs, and Sheets API calls.
type Client struct {
	gws *gws.Client
}

// NewClient creates a Drive client backed by the given gws.Client.
func NewClient(g *gws.Client) *Client {
	return &Client{gws: g}
}

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
func (c *Client) ListChanges(pageToken string) ([]Change, string, error) {
	var allChanges []Change
	var newPageToken string

	currentToken := pageToken
	for {
		params := gws.ParamsJSON(map[string]string{
			"pageToken": currentToken,
			"fields":    "changes(fileId,removed,file(name,mimeType,modifiedTime)),newStartPageToken,nextPageToken",
		})

		var resp changesResponse
		if err := c.gws.RunParsed(&resp, "drive", "changes", "list", "--params", params); err != nil {
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
func (c *Client) SeedPageToken() (string, error) {
	var resp seedPageTokenResponse
	if err := c.gws.RunParsed(&resp, "drive", "changes", "getStartPageToken"); err != nil {
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
func (c *Client) ListFiles(timeMin string) ([]Change, error) {
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
		if err := c.gws.RunParsed(&resp, "drive", "files", "list", "--params", gws.ParamsJSON(params)); err != nil {
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
func (c *Client) GetDocument(docID string) (*modelv1.Document, error) {
	// includeTabsContent is a boolean, so we use json.Marshal directly
	// instead of ParamsJSON (which only handles map[string]string).
	params := map[string]any{"documentId": docID, "includeTabsContent": true}
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("marshal doc params: %w", err)
	}

	var doc modelv1.Document
	if err := c.gws.RunParsed(&doc, "docs", "documents", "get", "--params", string(paramsJSON)); err != nil {
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
func (c *Client) GetSheetNames(spreadsheetID string) ([]string, error) {
	params := gws.ParamsJSON(map[string]string{
		"spreadsheetId": spreadsheetID,
		"fields":        "sheets.properties(sheetId,title,index)",
	})

	var resp sheetsMetaResponse
	if err := c.gws.RunParsed(&resp, "sheets", "spreadsheets", "get", "--params", params); err != nil {
		return nil, fmt.Errorf("get sheet names %s: %w", spreadsheetID, err)
	}

	var names []string
	for _, s := range resp.Sheets {
		names = append(names, s.Properties.Title)
	}
	return names, nil
}

// sheetValuesResponse uses json.RawMessage because the Sheets API returns
// mixed types per cell: strings are quoted, but numbers and booleans are
// bare JSON values. Using [][]string fails to unmarshal those bare values.
type sheetValuesResponse struct {
	Values [][]json.RawMessage `json:"values"`
}

// ReadSheetValues fetches cell values for a specific sheet range.
func (c *Client) ReadSheetValues(spreadsheetID, sheetName string) ([][]string, error) {
	return c.readSheetRange(spreadsheetID, sheetName, "FORMATTED_VALUE")
}

// ReadSheetFormulas fetches formulas for a specific sheet range.
// Cells with formulas return the formula string (e.g. "=SUM(A1:A10)");
// cells without formulas return the computed value.
func (c *Client) ReadSheetFormulas(spreadsheetID, sheetName string) ([][]string, error) {
	return c.readSheetRange(spreadsheetID, sheetName, "FORMULA")
}

func (c *Client) readSheetRange(spreadsheetID, sheetName, renderOption string) ([][]string, error) {
	params := gws.ParamsJSON(map[string]string{
		"spreadsheetId":     spreadsheetID,
		"range":             sheetName,
		"valueRenderOption": renderOption,
	})

	var resp sheetValuesResponse
	if err := c.gws.RunParsed(&resp, "sheets", "spreadsheets", "values", "get", "--params", params); err != nil {
		return nil, fmt.Errorf("read sheet %s %s/%s: %w", renderOption, spreadsheetID, sheetName, err)
	}

	rows := make([][]string, len(resp.Values))
	for i, raw := range resp.Values {
		row := make([]string, len(raw))
		for j, cell := range raw {
			row[j] = cellToString(cell)
		}
		rows[i] = row
	}
	return rows, nil
}

// cellToString converts a raw JSON cell value to a string. JSON strings are
// unquoted; numbers, booleans, and null are used as their literal text.
func cellToString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	if raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return s
		}
	}
	return string(raw)
}

// --- Comments API ---

// ListComments fetches all comments on a file, including replies. Paginates
// through all pages. Each returned DriveComment pairs a typed drive.Comment
// (for field access) with the raw API response map (for lossless storage).
// Replies are nested inside each comment — the API returns them that way
// and storage preserves that shape.
func (c *Client) ListComments(fileID string) ([]*modelv1.DriveComment, error) {
	params := map[string]string{
		"fileId": fileID,
		"fields": "comments,nextPageToken",
	}

	var all []*modelv1.DriveComment
	for {
		out, err := c.gws.Run("drive", "comments", "list", "--params", gws.ParamsJSON(params))
		if err != nil {
			return nil, fmt.Errorf("list comments for %s: %w", fileID, err)
		}

		var resp drive.CommentList
		if err := json.Unmarshal(out, &resp); err != nil {
			return nil, fmt.Errorf("parse comments for %s: %w", fileID, err)
		}

		var rawResp map[string]any
		if err := json.Unmarshal(out, &rawResp); err != nil {
			return nil, fmt.Errorf("parse comments for %s as map: %w", fileID, err)
		}

		rawComments, err := extractCommentItems(rawResp, len(resp.Comments))
		if err != nil {
			return nil, fmt.Errorf("extract comments for %s: %w", fileID, err)
		}

		for i, c := range resp.Comments {
			all = append(all, &modelv1.DriveComment{
				Runtime:    *c,
				Serialized: rawComments[i],
			})
		}

		if resp.NextPageToken != "" {
			params["pageToken"] = resp.NextPageToken
			continue
		}
		break
	}

	return all, nil
}

// extractCommentItems pulls the per-comment raw maps from a drive.CommentList
// response's "comments" array and validates the shape against the expected count.
func extractCommentItems(rawResp map[string]any, expected int) ([]map[string]any, error) {
	if expected == 0 {
		return nil, nil
	}
	rawItemsAny, ok := rawResp["comments"]
	if !ok || rawItemsAny == nil {
		return nil, fmt.Errorf("raw response missing comments field but typed response has %d comments", expected)
	}
	rawSlice, ok := rawItemsAny.([]any)
	if !ok {
		return nil, fmt.Errorf("raw comments is not an array: got %T", rawItemsAny)
	}
	if len(rawSlice) != expected {
		return nil, fmt.Errorf("raw comments count %d does not match typed comments count %d", len(rawSlice), expected)
	}
	result := make([]map[string]any, len(rawSlice))
	for i, item := range rawSlice {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("raw comments[%d] is not an object: got %T", i, item)
		}
		result[i] = m
	}
	return result, nil
}
