package outbox

import (
	"encoding/json"
	"testing"

	"github.com/anish749/pigeon/internal/store/modelv1"
)

func makeItem(platform string, via modelv1.Via, sessionID string) *Item {
	payload, _ := json.Marshal(map[string]string{
		"platform": platform,
		"via":      string(via),
	})
	return &Item{ID: "test-1", SessionID: sessionID, Payload: payload}
}

func TestVia(t *testing.T) {
	tests := []struct {
		name string
		via  modelv1.Via
		want modelv1.Via
	}{
		{"empty", "", ""},
		{"bot", modelv1.ViaPigeonAsBot, modelv1.ViaPigeonAsBot},
		{"user", modelv1.ViaPigeonAsUser, modelv1.ViaPigeonAsUser},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := makeItem("slack", tt.via, "")
			if got := item.Via(); got != tt.want {
				t.Fatalf("Via() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPlatform(t *testing.T) {
	for _, p := range []string{"slack", "whatsapp", ""} {
		t.Run(p, func(t *testing.T) {
			item := makeItem(p, "", "")
			if got := item.Platform(); got != p {
				t.Fatalf("Platform() = %q, want %q", got, p)
			}
		})
	}
}

func TestHasSession(t *testing.T) {
	if makeItem("slack", "", "").HasSession() {
		t.Fatal("HasSession() = true for empty SessionID")
	}
	if !makeItem("slack", "", "sess-1").HasSession() {
		t.Fatal("HasSession() = false for non-empty SessionID")
	}
}

func TestCycleVia(t *testing.T) {
	tests := []struct {
		name     string
		platform string
		via      modelv1.Via
		want     modelv1.Via
	}{
		{"slack empty to user", "slack", "", modelv1.ViaPigeonAsUser},
		{"slack bot to user", "slack", modelv1.ViaPigeonAsBot, modelv1.ViaPigeonAsUser},
		{"slack user to bot", "slack", modelv1.ViaPigeonAsUser, modelv1.ViaPigeonAsBot},
		{"whatsapp always empty", "whatsapp", "", ""},
		{"whatsapp ignores bot", "whatsapp", modelv1.ViaPigeonAsBot, ""},
		{"whatsapp ignores user", "whatsapp", modelv1.ViaPigeonAsUser, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := makeItem(tt.platform, tt.via, "")
			if got := item.CycleVia(); got != tt.want {
				t.Fatalf("CycleVia() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHeaderWithBrokenPayload(t *testing.T) {
	item := &Item{Payload: []byte("not json")}
	if got := item.Via(); got != "" {
		t.Fatalf("Via() on broken payload = %q, want empty", got)
	}
	if got := item.Platform(); got != "" {
		t.Fatalf("Platform() on broken payload = %q, want empty", got)
	}
	if got := item.CycleVia(); got != modelv1.ViaPigeonAsUser {
		t.Fatalf("CycleVia() on broken payload = %q, want %q", got, modelv1.ViaPigeonAsUser)
	}
}
