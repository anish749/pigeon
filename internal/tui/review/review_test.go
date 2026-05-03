package review

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/anish749/pigeon/internal/api"
	"github.com/anish749/pigeon/internal/outbox"
	"github.com/anish749/pigeon/internal/store/modelv1"
)

func TestSendIdentity(t *testing.T) {
	tests := []struct {
		name string
		req  api.SendRequest
		want string
	}{
		{name: "slack as bot via explicit", req: api.SendRequest{Platform: "slack", Via: modelv1.ViaPigeonAsBot}, want: "pigeon"},
		{name: "slack as bot via empty default", req: api.SendRequest{Platform: "slack"}, want: "pigeon"},
		{name: "slack as user", req: api.SendRequest{Platform: "slack", Via: modelv1.ViaPigeonAsUser}, want: "user"},
		{name: "whatsapp always user — via empty", req: api.SendRequest{Platform: "whatsapp"}, want: "user"},
		{name: "whatsapp always user — via bot ignored", req: api.SendRequest{Platform: "whatsapp", Via: modelv1.ViaPigeonAsBot}, want: "user"},
		{name: "whatsapp always user — via user", req: api.SendRequest{Platform: "whatsapp", Via: modelv1.ViaPigeonAsUser}, want: "user"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sendIdentity(tt.req); got != tt.want {
				t.Fatalf("sendIdentity() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestItemSummary(t *testing.T) {
	tests := []struct {
		name string
		req  api.SendRequest
		want string
	}{
		{
			name: "slack as bot shows from",
			req:  api.SendRequest{Platform: "slack", Slack: &api.SlackTarget{Channel: "#eng"}, Message: "hello", Via: modelv1.ViaPigeonAsBot},
			want: "slack → #eng (from pigeon): hello",
		},
		{
			name: "slack as user shows from",
			req:  api.SendRequest{Platform: "slack", Slack: &api.SlackTarget{UserID: "U123"}, Message: "hello", Via: modelv1.ViaPigeonAsUser},
			want: "slack → U123 (from user): hello",
		},
		{
			name: "slack empty via defaults to bot",
			req:  api.SendRequest{Platform: "slack", Slack: &api.SlackTarget{Channel: "#eng"}, Message: "hello"},
			want: "slack → #eng (from pigeon): hello",
		},
		{
			name: "whatsapp omits from",
			req:  api.SendRequest{Platform: "whatsapp", Contact: "Alice", Message: "hello"},
			want: "whatsapp → Alice: hello",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := itemFromReq(t, tt.req)
			if got := itemSummary(item); got != tt.want {
				t.Fatalf("itemSummary() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestItemSummaryTruncatesLongMessage(t *testing.T) {
	longMsg := strings.Repeat("a", 100)
	req := api.SendRequest{Platform: "slack", Slack: &api.SlackTarget{Channel: "#eng"}, Message: longMsg, Via: modelv1.ViaPigeonAsBot}
	got := itemSummary(itemFromReq(t, req))
	want := "slack → #eng (from pigeon): " + strings.Repeat("a", 57) + "..."
	if got != want {
		t.Fatalf("itemSummary() = %q, want %q", got, want)
	}
}

func TestItemSummaryUnparseablePayload(t *testing.T) {
	item := &outbox.Item{Payload: []byte("not json")}
	if got := itemSummary(item); got != "(unknown)" {
		t.Fatalf("itemSummary() = %q, want %q", got, "(unknown)")
	}
}

func itemFromReq(t *testing.T, req api.SendRequest) *outbox.Item {
	t.Helper()
	payload, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	return &outbox.Item{Payload: payload}
}

func TestCycleVia(t *testing.T) {
	tests := []struct {
		name string
		req  api.SendRequest
		want modelv1.Via
	}{
		{name: "slack bot to user", req: api.SendRequest{Platform: "slack", Via: modelv1.ViaPigeonAsBot}, want: modelv1.ViaPigeonAsUser},
		{name: "slack empty to user", req: api.SendRequest{Platform: "slack"}, want: modelv1.ViaPigeonAsUser},
		{name: "slack user to bot", req: api.SendRequest{Platform: "slack", Via: modelv1.ViaPigeonAsUser}, want: modelv1.ViaPigeonAsBot},
		{name: "whatsapp always empty", req: api.SendRequest{Platform: "whatsapp"}, want: ""},
		{name: "whatsapp via bot still empty", req: api.SendRequest{Platform: "whatsapp", Via: modelv1.ViaPigeonAsBot}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := itemFromReq(t, tt.req)
			if got := cycleVia(item); got != tt.want {
				t.Fatalf("cycleVia() = %q, want %q", got, tt.want)
			}
		})
	}
}
