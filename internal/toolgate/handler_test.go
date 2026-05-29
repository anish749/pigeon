package toolgate

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHandleHook(t *testing.T) {
	g := NewGate()
	h := NewHandler(g, nil, 5*time.Second)

	input := makeInput("Bash", map[string]string{"command": "ls"})
	body, _ := json.Marshal(input)

	done := make(chan struct{})
	var rec *httptest.ResponseRecorder

	go func() {
		defer close(done)
		req := httptest.NewRequest(http.MethodPost, "/api/hook/pretooluse", bytes.NewReader(body))
		rec = httptest.NewRecorder()
		h.HandleHook(rec, req)
	}()

	// Wait for the item to appear in the gate.
	var items []*Item
	for i := 0; i < 50; i++ {
		items = g.List()
		if len(items) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(items) == 0 {
		t.Fatal("item never appeared in gate")
	}

	g.Resolve(items[0].ID, Decision{Action: "allow", Reason: "approved"})

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("HandleHook did not return")
	}

	var out HookOutput
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if out.HookSpecificOutput == nil {
		t.Fatal("hookSpecificOutput is nil")
	}
	if out.HookSpecificOutput.PermissionDecision != "allow" {
		t.Fatalf("decision = %q, want allow", out.HookSpecificOutput.PermissionDecision)
	}
}

func TestHandleHookTimeout(t *testing.T) {
	g := NewGate()
	h := NewHandler(g, nil, 50*time.Millisecond)

	input := makeInput("Bash", map[string]string{"command": "dangerous"})
	body, _ := json.Marshal(input)

	req := httptest.NewRequest(http.MethodPost, "/api/hook/pretooluse", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.HandleHook(rec, req)

	var out HookOutput
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if out.HookSpecificOutput == nil {
		t.Fatal("hookSpecificOutput is nil")
	}
	if out.HookSpecificOutput.PermissionDecision != "ask" {
		t.Fatalf("decision = %q on timeout, want ask", out.HookSpecificOutput.PermissionDecision)
	}
}

func TestHandleList(t *testing.T) {
	g := NewGate()
	h := NewHandler(g, nil, time.Minute)

	g.Submit(makeInput("Bash", map[string]string{"command": "ls"}))
	g.Submit(makeInput("Read", map[string]string{"file_path": "/etc/passwd"}))

	req := httptest.NewRequest(http.MethodGet, "/api/toolgate", nil)
	rec := httptest.NewRecorder()
	h.HandleList(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var items []json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatalf("unmarshal list: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("list returned %d items, want 2", len(items))
	}
}

func TestHandleAction(t *testing.T) {
	g := NewGate()
	h := NewHandler(g, nil, 5*time.Second)

	item := g.Submit(makeInput("Bash", map[string]string{"command": "rm -rf /"}))

	// Drain the decision in a goroutine.
	go func() { <-item.decision }()

	actionBody, _ := json.Marshal(ActionRequest{
		ID:     item.ID,
		Action: "deny",
		Reason: "dangerous",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/toolgate/action", bytes.NewReader(actionBody))
	rec := httptest.NewRecorder()
	h.HandleAction(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp ActionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal action response: %v", err)
	}
	if !resp.OK {
		t.Fatalf("OK = false, error = %q", resp.Error)
	}
}

func TestHandleActionNotFound(t *testing.T) {
	g := NewGate()
	h := NewHandler(g, nil, time.Minute)

	actionBody, _ := json.Marshal(ActionRequest{
		ID:     "nonexistent",
		Action: "allow",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/toolgate/action", bytes.NewReader(actionBody))
	rec := httptest.NewRecorder()
	h.HandleAction(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}

	var resp ActionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal action response: %v", err)
	}
	if resp.OK {
		t.Fatal("OK = true on not-found, want false")
	}
}

func TestHandleHookNotify(t *testing.T) {
	g := NewGate()
	notified := make(chan string, 1)
	notify := func(_ context.Context, item *Item) error {
		notified <- item.ID
		return nil
	}
	h := NewHandler(g, notify, 5*time.Second)

	input := makeInput("Write", map[string]string{"file_path": "/tmp/x"})
	body, _ := json.Marshal(input)

	done := make(chan struct{})
	go func() {
		defer close(done)
		req := httptest.NewRequest(http.MethodPost, "/api/hook/pretooluse", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		h.HandleHook(rec, req)
	}()

	// Wait for notification.
	select {
	case id := <-notified:
		// Resolve so the handler unblocks.
		g.Resolve(id, Decision{Action: "allow"})
	case <-time.After(2 * time.Second):
		t.Fatal("notify callback never called")
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("HandleHook did not return after resolve")
	}
}
