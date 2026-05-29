package toolgate

import (
	"encoding/json"
	"testing"
	"time"
)

func makeInput(tool string, input map[string]string) HookInput {
	raw, _ := json.Marshal(input)
	return HookInput{
		SessionID: "sess-1",
		ToolName:  tool,
		ToolInput: raw,
	}
}

func TestSubmitAndResolve(t *testing.T) {
	g := NewGate()
	item := g.Submit(makeInput("Bash", map[string]string{"command": "ls"}))

	go func() {
		time.Sleep(10 * time.Millisecond)
		g.Resolve(item.ID, Decision{Action: "allow", Reason: "ok"})
	}()

	select {
	case d := <-item.decision:
		if d.Action != "allow" {
			t.Fatalf("Action = %q, want allow", d.Action)
		}
		if d.Reason != "ok" {
			t.Fatalf("Reason = %q, want ok", d.Reason)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for decision")
	}
}

func TestList(t *testing.T) {
	g := NewGate()
	a := g.Submit(makeInput("Bash", map[string]string{"command": "ls"}))
	b := g.Submit(makeInput("Read", map[string]string{"file_path": "/tmp/x"}))
	c := g.Submit(makeInput("Glob", map[string]string{"pattern": "*.go"}))

	items := g.List()
	if len(items) != 3 {
		t.Fatalf("List() returned %d items, want 3", len(items))
	}
	if items[0].ID != a.ID || items[1].ID != b.ID || items[2].ID != c.ID {
		t.Fatalf("List() order = [%s, %s, %s], want [%s, %s, %s]",
			items[0].ID, items[1].ID, items[2].ID, a.ID, b.ID, c.ID)
	}
}

func TestResolveRemovesItem(t *testing.T) {
	g := NewGate()
	item := g.Submit(makeInput("Bash", map[string]string{"command": "ls"}))

	// Drain the decision channel in a goroutine so Resolve doesn't block.
	go func() { <-item.decision }()

	if !g.Resolve(item.ID, Decision{Action: "deny"}) {
		t.Fatal("Resolve returned false on existing item")
	}

	if got := g.Get(item.ID); got != nil {
		t.Fatalf("Get(%s) = %v after Resolve, want nil", item.ID, got)
	}
	if items := g.List(); len(items) != 0 {
		t.Fatalf("List() has %d items after Resolve, want 0", len(items))
	}
}

func TestRemoveWithoutResolve(t *testing.T) {
	g := NewGate()
	item := g.Submit(makeInput("Bash", map[string]string{"command": "rm -rf /"}))

	if !g.Remove(item.ID) {
		t.Fatal("Remove returned false on existing item")
	}
	if got := g.Get(item.ID); got != nil {
		t.Fatalf("Get(%s) = %v after Remove, want nil", item.ID, got)
	}

	// decision channel was never sent to — verify no panic on select.
	select {
	case <-item.decision:
		t.Fatal("decision channel should be empty after Remove")
	default:
	}
}

func TestResolveNotFound(t *testing.T) {
	g := NewGate()
	if g.Resolve("nonexistent", Decision{Action: "allow"}) {
		t.Fatal("Resolve returned true for unknown ID")
	}
}

func TestItemCommand(t *testing.T) {
	tests := []struct {
		name string
		tool string
		inp  map[string]string
		want string
	}{
		{"bash", "Bash", map[string]string{"command": "echo hello"}, "echo hello"},
		{"read", "Read", map[string]string{"file_path": "/tmp/foo.txt"}, "/tmp/foo.txt"},
		{"glob", "Glob", map[string]string{"pattern": "**/*.go"}, "**/*.go"},
		{"grep", "Grep", map[string]string{"pattern": "TODO"}, "TODO"},
		{"unknown", "CustomTool", map[string]string{"x": "y"}, `{"x":"y"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := &Item{Input: makeInput(tt.tool, tt.inp)}
			if got := item.Command(); got != tt.want {
				t.Fatalf("Command() = %q, want %q", got, tt.want)
			}
		})
	}
}
