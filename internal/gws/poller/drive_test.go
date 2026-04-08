package poller

import "testing"

func TestDriveSlug(t *testing.T) {
	tests := []struct {
		title  string
		fileID string
		want   string
	}{
		{"Meeting Notes", "1BxiMVs0XYZ", "meeting-notes-1BxiMVs0XYZ"},
		{"", "1BxiMVs0XYZ", "1BxiMVs0XYZ"},
		{"Q2 Budget", "abc123", "q2-budget-abc123"},
		{"already-slugged", "id1", "already-slugged-id1"},
		{"  Spaces  Everywhere  ", "f1", "spaces-everywhere-f1"},
	}
	for _, tt := range tests {
		got := driveSlug(tt.title, tt.fileID)
		if got != tt.want {
			t.Errorf("driveSlug(%q, %q) = %q, want %q", tt.title, tt.fileID, got, tt.want)
		}
	}
}
