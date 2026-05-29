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

func TestToolGateCommand(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		input    string
		want     string
	}{
		{
			name:     "bash command",
			toolName: "Bash",
			input:    `{"command":"rm -rf /tmp/cache"}`,
			want:     "rm -rf /tmp/cache",
		},
		{
			name:     "read file_path",
			toolName: "Read",
			input:    `{"file_path":"/src/main.go"}`,
			want:     "/src/main.go",
		},
		{
			name:     "edit file_path",
			toolName: "Edit",
			input:    `{"file_path":"/src/main.go","old_string":"foo","new_string":"bar"}`,
			want:     "/src/main.go",
		},
		{
			name:     "write file_path",
			toolName: "Write",
			input:    `{"file_path":"/tmp/out.txt","content":"hello"}`,
			want:     "/tmp/out.txt",
		},
		{
			name:     "glob pattern",
			toolName: "Glob",
			input:    `{"pattern":"**/*.go"}`,
			want:     "**/*.go",
		},
		{
			name:     "grep pattern",
			toolName: "Grep",
			input:    `{"pattern":"func main"}`,
			want:     "func main",
		},
		{
			name:     "unknown tool shows raw json",
			toolName: "WebFetch",
			input:    `{"url":"https://example.com"}`,
			want:     `{"url":"https://example.com"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tg := &toolGateListItem{
				Input: toolGateInput{
					ToolName:  tt.toolName,
					ToolInput: json.RawMessage(tt.input),
				},
			}
			if got := toolGateCommand(tg); got != tt.want {
				t.Fatalf("toolGateCommand() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestToolGateSummary(t *testing.T) {
	tg := &toolGateListItem{
		Input: toolGateInput{
			ToolName:  "Bash",
			ToolInput: json.RawMessage(`{"command":"ls -la"}`),
		},
	}
	got := toolGateSummary(tg)
	if !strings.Contains(got, "[TOOL]") {
		t.Fatalf("toolGateSummary() = %q, expected to contain [TOOL]", got)
	}
	if !strings.Contains(got, "Bash") {
		t.Fatalf("toolGateSummary() = %q, expected to contain Bash", got)
	}
	if !strings.Contains(got, "ls -la") {
		t.Fatalf("toolGateSummary() = %q, expected to contain ls -la", got)
	}
}

func TestToolGateSummaryTruncates(t *testing.T) {
	longCmd := strings.Repeat("x", 100)
	tg := &toolGateListItem{
		Input: toolGateInput{
			ToolName:  "Bash",
			ToolInput: json.RawMessage(`{"command":"` + longCmd + `"}`),
		},
	}
	got := toolGateSummary(tg)
	if !strings.Contains(got, "...") {
		t.Fatalf("toolGateSummary() = %q, expected truncation", got)
	}
}

func TestListItemID(t *testing.T) {
	obItem := listItem{outboxItem: &outbox.Item{ID: "ob1"}}
	tgItem := listItem{toolGateItem: &toolGateListItem{ID: "tg1"}}

	if got := obItem.id(); got != "ob1" {
		t.Fatalf("outbox listItem.id() = %q, want ob1", got)
	}
	if got := tgItem.id(); got != "tg1" {
		t.Fatalf("toolgate listItem.id() = %q, want tg1", got)
	}
}

func TestListItemIsToolGate(t *testing.T) {
	obItem := listItem{outboxItem: &outbox.Item{}}
	tgItem := listItem{toolGateItem: &toolGateListItem{}}

	if obItem.isToolGate() {
		t.Fatal("outbox item should not be tool gate")
	}
	if !tgItem.isToolGate() {
		t.Fatal("tool gate item should be tool gate")
	}
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
			if got := item.CycleVia(); got != tt.want {
				t.Fatalf("CycleVia() = %q, want %q", got, tt.want)
			}
		})
	}
}
