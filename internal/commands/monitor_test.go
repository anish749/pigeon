package commands

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/tailapi"
)

// startFakeDaemon brings up a Unix socket HTTP server at paths.SocketPath()
// and returns a function that shuts it down. The handler argument runs
// for every request so each test can serve whatever it needs.
//
// Uses a short temp dir because macOS limits unix socket paths to ~104
// bytes — the default t.TempDir() path can exceed that.
func startFakeDaemon(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "pgn-test-")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	t.Setenv("PIGEON_STATE_DIR", dir)

	sockPath := paths.SocketPath()
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen unix %s: %v", sockPath, err)
	}

	srv := &http.Server{Handler: handler}
	go srv.Serve(ln)

	t.Cleanup(func() {
		_ = srv.Close()
		_ = ln.Close()
	})
}

func TestRunMonitor_WritesFramesToOut(t *testing.T) {
	// Fake daemon that emits two SSE data frames then closes.
	startFakeDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tail" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher := w.(http.Flusher)
		fmt.Fprint(w, "data: {\"kind\":\"system\",\"content\":\"connected\"}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: {\"kind\":\"message\",\"content\":\"hi\"}\n\n")
		flusher.Flush()
	})

	var buf bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := RunMonitor(ctx, tailapi.Request{}, &buf)
	if err != nil {
		t.Fatalf("RunMonitor: %v", err)
	}

	got := buf.String()
	wantLines := []string{
		`{"kind":"system","content":"connected"}`,
		`{"kind":"message","content":"hi"}`,
	}
	for _, want := range wantLines {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\nfull output:\n%s", want, got)
		}
	}
}

func TestRunMonitor_EncodesRequestIntoQuery(t *testing.T) {
	// Capture the query the daemon sees.
	var gotRequest tailapi.Request
	var decodeErr error

	startFakeDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		gotRequest, decodeErr = tailapi.Decode(r.URL.Query())
		w.Header().Set("Content-Type", "text/event-stream")
		w.(http.Flusher).Flush()
	})

	acme := account.New("slack", "acme")
	sent := tailapi.Request{
		Accounts: []account.Account{acme},
		Since:    time.Date(2026, 4, 23, 12, 0, 0, 0, time.UTC),
	}

	var buf bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	_ = RunMonitor(ctx, sent, &buf) // may error on context deadline, we don't care

	if decodeErr != nil {
		t.Fatalf("server decode: %v", decodeErr)
	}
	if len(gotRequest.Accounts) != 1 || gotRequest.Accounts[0] != acme {
		t.Errorf("accounts mismatch: got %+v, want [%v]", gotRequest.Accounts, acme)
	}
	if !gotRequest.Since.Equal(sent.Since) {
		t.Errorf("since mismatch: got %v, want %v", gotRequest.Since, sent.Since)
	}
}

func TestRunMonitor_NonOKStatusReturnsError(t *testing.T) {
	tests := []struct {
		name           string
		status         int
		body           string
		wantInErrorMsg string
	}{
		{
			name:           "bad request surfaces body",
			status:         http.StatusBadRequest,
			body:           "invalid request",
			wantInErrorMsg: "daemon returned 400",
		},
		{
			name:           "server error surfaces body",
			status:         http.StatusInternalServerError,
			body:           "kaboom",
			wantInErrorMsg: "daemon returned 500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			startFakeDaemon(t, func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, tt.body, tt.status)
			})

			var buf bytes.Buffer
			err := RunMonitor(context.Background(), tailapi.Request{}, &buf)
			if err == nil {
				t.Fatalf("expected error, got nil (output: %q)", buf.String())
			}
			if !strings.Contains(err.Error(), tt.wantInErrorMsg) {
				t.Errorf("error %q does not contain %q", err, tt.wantInErrorMsg)
			}
			if !strings.Contains(err.Error(), tt.body) {
				t.Errorf("error %q does not contain body %q", err, tt.body)
			}
		})
	}
}

func TestRunMonitor_SkipsNonDataLines(t *testing.T) {
	startFakeDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		// Mix of SSE comments, empty lines, event: lines, and data: lines.
		fmt.Fprint(w, ": this is a comment\n\n")
		fmt.Fprint(w, "event: custom\ndata: {\"kind\":\"message\"}\n\n")
		fmt.Fprint(w, "data: {\"kind\":\"reaction\"}\n\n")
		flusher.Flush()
	})

	var buf bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	_ = RunMonitor(ctx, tailapi.Request{}, &buf)

	// Only `data:` lines should make it through. Event: lines and
	// comments are dropped.
	out := buf.String()
	if !strings.Contains(out, `{"kind":"message"}`) {
		t.Errorf("missing message frame: %q", out)
	}
	if !strings.Contains(out, `{"kind":"reaction"}`) {
		t.Errorf("missing reaction frame: %q", out)
	}
	if strings.Contains(out, "comment") || strings.Contains(out, "event:") {
		t.Errorf("unexpected non-data content leaked: %q", out)
	}
}
