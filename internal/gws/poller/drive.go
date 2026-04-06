package poller

import (
	"fmt"
	"log/slog"

	"github.com/anish749/pigeon/internal/gws"
)

type driveStartToken struct {
	StartPageToken string `json:"startPageToken"`
}

type driveChangesResponse struct {
	Changes       []driveChange `json:"changes"`
	NewStartToken string        `json:"newStartPageToken"`
	NextPageToken string        `json:"nextPageToken"`
}

type driveChange struct {
	FileID  string    `json:"fileId"`
	Removed bool      `json:"removed"`
	File    driveFile `json:"file"`
}

type driveFile struct {
	Name     string `json:"name"`
	MimeType string `json:"mimeType"`
}

// PollDrive checks for changed files since the stored pageToken.
// On first run (no cursor), it seeds the pageToken.
func PollDrive(cursors *Cursors) error {
	if cursors.Drive.PageToken == "" {
		return seedDriveCursor(cursors)
	}

	var resp driveChangesResponse
	err := gws.RunParsed(&resp,
		"drive", "changes", "list",
		"--params", gws.ParamsJSON(map[string]string{
			"pageToken":         cursors.Drive.PageToken,
			"includeRemoved":    "true",
			"restrictToMyDrive": "true",
			"fields":            "changes(fileId,removed,file(name,mimeType)),newStartPageToken,nextPageToken",
		}),
	)
	if err != nil {
		return fmt.Errorf("poll drive: %w", err)
	}

	for _, change := range resp.Changes {
		if change.Removed {
			slog.Info("drive: file removed", "file_id", change.FileID)
			continue
		}
		slog.Info("drive: file changed",
			"file_id", change.FileID,
			"name", change.File.Name,
			"mime_type", change.File.MimeType,
		)
	}

	if len(resp.Changes) > 0 {
		slog.Info("drive: poll complete", "changes", len(resp.Changes))
	}

	if resp.NewStartToken != "" {
		cursors.Drive.PageToken = resp.NewStartToken
	}
	return nil
}

func seedDriveCursor(cursors *Cursors) error {
	var resp driveStartToken
	err := gws.RunParsed(&resp, "drive", "changes", "getStartPageToken")
	if err != nil {
		return fmt.Errorf("seed drive cursor: %w", err)
	}
	cursors.Drive.PageToken = resp.StartPageToken
	slog.Info("drive: seeded cursor", "page_token", resp.StartPageToken)
	return nil
}
