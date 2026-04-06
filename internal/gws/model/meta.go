package model

// DocMeta holds metadata about a synced Google Drive document.
type DocMeta struct {
	FileID       string    `json:"fileId"`
	MimeType     string    `json:"mimeType"`
	Title        string    `json:"title"`
	ModifiedTime string    `json:"modifiedTime"`
	SyncedAt     string    `json:"syncedAt"`
	Tabs         []TabMeta `json:"tabs,omitempty"`
	Sheets       []string  `json:"sheets,omitempty"`
}

// TabMeta holds metadata about a single tab within a document.
type TabMeta struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}
