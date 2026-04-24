package hub

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
	"github.com/anish749/pigeon/internal/store/modelv1"
	"github.com/anish749/pigeon/internal/tailapi"
)

// newTestHub builds a Hub rooted at a tmp data dir with no sessions.
// Isolates PIGEON_STATE_DIR so claude.ListAllSessions doesn't pick up
// the developer's real session files.
func newTestHub(t *testing.T) (*Hub, store.Store, paths.DataRoot) {
	t.Helper()
	t.Setenv("PIGEON_STATE_DIR", t.TempDir())

	dir := t.TempDir()
	root := paths.NewDataRoot(dir)
	s := store.NewFSStore(root)
	h, err := New(context.Background(), s, root)
	if err != nil {
		t.Fatalf("hub.New: %v", err)
	}
	t.Cleanup(h.Stop)
	return h, s, root
}

// collectFrames starts the tail handler against an httptest server,
// reads until readFor elapses or ctx is cancelled, and returns the
// decoded frames.
func collectFrames(t *testing.T, h *Hub, req tailapi.Request, readFor time.Duration, onConnected func()) []map[string]any {
	t.Helper()
	srv := httptest.NewServer(h.TailHandler())
	t.Cleanup(srv.Close)

	q, err := req.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	url := srv.URL
	if enc := q.Encode(); enc != "" {
		url += "?" + enc
	}

	ctx, cancel := context.WithTimeout(context.Background(), readFor)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	var frames []map[string]any
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	connectedFired := false
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var f map[string]any
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &f); err != nil {
			t.Fatalf("unmarshal frame: %v", err)
		}
		frames = append(frames, f)

		// Trigger the caller's post-connect hook once the system
		// "connected" frame has been observed.
		if !connectedFired && f["kind"] == "system" && f["content"] == "connected" && onConnected != nil {
			connectedFired = true
			go onConnected()
		}
	}
	return frames
}

func TestTailHandler_EmitsConnectedFrame(t *testing.T) {
	h, _, _ := newTestHub(t)

	frames := collectFrames(t, h, tailapi.Request{}, 200*time.Millisecond, nil)
	if len(frames) == 0 {
		t.Fatal("expected at least the connected frame, got none")
	}
	first := frames[0]
	if first["kind"] != "system" {
		t.Errorf("first frame kind = %v, want system", first["kind"])
	}
	if first["content"] != "connected" {
		t.Errorf("first frame content = %v, want connected", first["content"])
	}
}

func TestTailHandler_LiveEventsFlow(t *testing.T) {
	h, _, _ := newTestHub(t)
	acct := account.New("slack", "acme")

	// Publish a live message after the client sees the connected frame.
	onConnect := func() {
		// Small delay so Subscribe has settled before we Publish.
		time.Sleep(10 * time.Millisecond)
		h.Route(acct, "#general", modelv1.MsgLine{
			ID:     "abc.123",
			Ts:     time.Now(),
			Sender: "alice",
			Text:   "hello world",
		})
	}

	frames := collectFrames(t, h, tailapi.Request{}, 500*time.Millisecond, onConnect)

	var gotMsg bool
	for _, f := range frames {
		if f["kind"] == "message" && f["msg_id"] == "abc.123" {
			gotMsg = true
			acctMap, _ := f["account"].(map[string]any)
			if acctMap["platform"] != "slack" || acctMap["name"] != "acme" {
				t.Errorf("platform/account wrong: %+v", f)
			}
			if f["conversation"] != "#general" {
				t.Errorf("conversation wrong: %+v", f)
			}
			if !strings.Contains(f["content"].(string), "hello world") {
				t.Errorf("content missing text: %+v", f["content"])
			}
		}
	}
	if !gotMsg {
		t.Errorf("did not receive the published message; frames: %+v", frames)
	}
}

func TestTailHandler_FilterScopesToAccount(t *testing.T) {
	h, _, _ := newTestHub(t)
	acme := account.New("slack", "acme")
	other := account.New("slack", "other")

	onConnect := func() {
		time.Sleep(10 * time.Millisecond)
		h.Route(acme, "#a", modelv1.MsgLine{ID: "1", Ts: time.Now(), Text: "from acme"})
		h.Route(other, "#b", modelv1.MsgLine{ID: "2", Ts: time.Now(), Text: "from other"})
	}

	frames := collectFrames(t, h, tailapi.Request{
		Accounts: []account.Account{acme},
	}, 500*time.Millisecond, onConnect)

	for _, f := range frames {
		if f["kind"] != "message" {
			continue
		}
		if acctMap, _ := f["account"].(map[string]any); acctMap["name"] == "other" {
			t.Errorf("filter leaked: got event for account=other: %+v", f)
		}
	}

	// Sanity: the acme event did come through.
	var sawAcme bool
	for _, f := range frames {
		if f["kind"] != "message" {
			continue
		}
		if acctMap, _ := f["account"].(map[string]any); acctMap["name"] == "acme" {
			sawAcme = true
		}
	}
	if !sawAcme {
		t.Errorf("expected acme message, got frames: %+v", frames)
	}
}

func TestTailHandler_InvalidRequestReturnsBadRequest(t *testing.T) {
	h, _, _ := newTestHub(t)
	srv := httptest.NewServer(h.TailHandler())
	defer srv.Close()

	// Garbage q= value that can't decode.
	resp, err := http.Get(srv.URL + "?q=!!!not-base64!!!")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}
