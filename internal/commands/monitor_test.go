package commands

import (
	"context"
	"fmt"
	"io"
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

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := RunMonitor(ctx, tailapi.Request{}, w); err != nil {
		t.Fatalf("RunMonitor: %v", err)
	}
	w.Close()
	out, _ := io.ReadAll(r)
	r.Close()

	got := string(out)
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

	_, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer w.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	_ = RunMonitor(ctx, sent, w) // may error on context deadline, we don't care

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

			_, w, err := os.Pipe()
			if err != nil {
				t.Fatalf("pipe: %v", err)
			}
			defer w.Close()
			runErr := RunMonitor(context.Background(), tailapi.Request{}, w)
			if runErr == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(runErr.Error(), tt.wantInErrorMsg) {
				t.Errorf("error %q does not contain %q", runErr, tt.wantInErrorMsg)
			}
			if !strings.Contains(runErr.Error(), tt.body) {
				t.Errorf("error %q does not contain body %q", runErr, tt.body)
			}
		})
	}
}

func TestRunMonitor_HandlesLargeFrame(t *testing.T) {
	// 4 MiB payload — larger than the old 1 MiB bufio.Scanner cap.
	// The new bufio.Reader path has no fixed size limit.
	const payloadSize = 4 * 1024 * 1024
	payload := strings.Repeat("x", payloadSize)

	startFakeDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		fmt.Fprintf(w, "data: %s\n\n", payload)
		flusher.Flush()
	})

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Drain the read end concurrently so the pipe doesn't block the writer
	// at 64 KiB (the kernel pipe buffer is much smaller than the payload).
	done := make(chan []byte, 1)
	go func() {
		out, _ := io.ReadAll(r)
		done <- out
	}()

	if err := RunMonitor(ctx, tailapi.Request{}, w); err != nil {
		t.Fatalf("RunMonitor: %v", err)
	}
	w.Close()
	out := <-done
	r.Close()

	// Expect the full payload followed by a single trailing newline from Fprintln.
	want := payload + "\n"
	if len(out) != len(want) {
		t.Fatalf("output length mismatch: got %d bytes, want %d", len(out), len(want))
	}
	if string(out) != want {
		t.Errorf("payload content mismatch (first 64 bytes got=%q want=%q)",
			string(out[:64]), want[:64])
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

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	_ = RunMonitor(ctx, tailapi.Request{}, w)
	w.Close()
	outBytes, _ := io.ReadAll(r)
	r.Close()

	// Only `data:` lines should make it through. Event: lines and
	// comments are dropped.
	out := string(outBytes)
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
