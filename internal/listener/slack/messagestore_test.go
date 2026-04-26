package slack

import (
	"os"
	"strings"
	"testing"
	"time"

	goslack "github.com/slack-go/slack"

	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store/modelv1"
	"github.com/anish749/pigeon/internal/store/modelv1/slackraw"
)

// TestWriteThreadMessage_StampsThreadFields round-trips through the store
// and asserts the persisted JSONL line carries both thread_ts and
// thread_id (set to the same parent value for Slack) on replies, and
// neither on parent records.
func TestWriteThreadMessage_StampsThreadFields(t *testing.T) {
	tests := []struct {
		name     string
		isReply  bool
		threadTS string
		want     string // expected value of both ThreadTS and ThreadID after read-back ("" = unset)
	}{
		{
			name:     "reply gets thread_ts and thread_id",
			isReply:  true,
			threadTS: "1700000001.000001",
			want:     "1700000001.000001",
		},
		{
			name:     "parent record (isReply=false) gets neither",
			isReply:  false,
			threadTS: "1700000001.000001",
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ms, s, acct := newTestMessageStore(t)
			rs := ResolvedSender{
				ChannelName: "#general", SenderName: "Alice", SenderID: "U001",
			}
			raw := slackraw.NewSlackRawContent(goslack.Msg{})

			written, err := ms.WriteThreadMessage(rs, tt.threadTS, "hi",
				time.Unix(1700000010, 0), "1700000010.000010", tt.isReply,
				modelv1.ViaOrganic, raw)
			if err != nil {
				t.Fatalf("WriteThreadMessage: %v", err)
			}
			if written.ThreadTS != tt.want {
				t.Errorf("returned MsgLine.ThreadTS = %q, want %q", written.ThreadTS, tt.want)
			}
			if written.ThreadID != tt.want {
				t.Errorf("returned MsgLine.ThreadID = %q, want %q", written.ThreadID, tt.want)
			}

			tf, err := s.ReadThread(acct, "#general", tt.threadTS)
			if err != nil {
				t.Fatalf("ReadThread: %v", err)
			}
			if tf == nil {
				t.Fatal("ReadThread returned nil")
			}
			// The line we just wrote is the parent if isReply=false (it was
			// the first write), or the first reply if isReply=true.
			var got modelv1.MsgLine
			if tt.isReply {
				if len(tf.Replies) != 1 {
					t.Fatalf("replies = %d, want 1", len(tf.Replies))
				}
				got = tf.Replies[0].MsgLine
			} else {
				got = tf.Parent.MsgLine
			}
			if got.ThreadTS != tt.want {
				t.Errorf("read-back MsgLine.ThreadTS = %q, want %q", got.ThreadTS, tt.want)
			}
			if got.ThreadID != tt.want {
				t.Errorf("read-back MsgLine.ThreadID = %q, want %q", got.ThreadID, tt.want)
			}
		})
	}
}

// TestAppendEdit_ThreadFields verifies edits stamp both thread_ts and
// thread_id on disk when the target lives in a thread, and emit neither
// when the target is a top-level message. The date file is the
// destination either way — only the line content changes.
func TestAppendEdit_ThreadFields(t *testing.T) {
	tests := []struct {
		name     string
		threadTS string
		wantKeys bool
	}{
		{name: "thread reply edit stamps both keys", threadTS: "1700000001.000001", wantKeys: true},
		{name: "top-level edit emits neither key", threadTS: "", wantKeys: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ms, _, acct := newTestMessageStore(t)
			rs := ResolvedSender{ChannelName: "#general", SenderName: "Alice", SenderID: "U001"}
			raw := slackraw.NewSlackRawContent(goslack.Msg{})

			editTime := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
			if err := ms.AppendEdit(rs, "1700000010.000010", tt.threadTS,
				"new text", editTime, raw); err != nil {
				t.Fatalf("AppendEdit: %v", err)
			}

			path := paths.DefaultDataRoot().AccountFor(acct).Conversation("#general").DateFile("2026-04-26").Path()
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read date file: %v", err)
			}
			line := strings.TrimRight(string(data), "\n")

			if tt.wantKeys {
				if !strings.Contains(line, `"thread_ts":"`+tt.threadTS+`"`) {
					t.Errorf("expected thread_ts on edit line; got: %s", line)
				}
				if !strings.Contains(line, `"thread_id":"`+tt.threadTS+`"`) {
					t.Errorf("expected thread_id on edit line; got: %s", line)
				}
			} else {
				if strings.Contains(line, "thread_ts") || strings.Contains(line, "thread_id") {
					t.Errorf("non-thread edit must omit thread_ts/thread_id; got: %s", line)
				}
			}
		})
	}
}

// TestAppendDelete_ThreadFields mirrors TestAppendEdit_ThreadFields for
// the delete path.
func TestAppendDelete_ThreadFields(t *testing.T) {
	tests := []struct {
		name     string
		threadTS string
		wantKeys bool
	}{
		{name: "thread reply delete stamps both keys", threadTS: "1700000001.000001", wantKeys: true},
		{name: "top-level delete emits neither key", threadTS: "", wantKeys: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ms, _, acct := newTestMessageStore(t)
			rs := ResolvedSender{ChannelName: "#general", SenderName: "Alice", SenderID: "U001"}

			delTime := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
			if err := ms.AppendDelete(rs, "1700000010.000010", tt.threadTS, delTime); err != nil {
				t.Fatalf("AppendDelete: %v", err)
			}

			path := paths.DefaultDataRoot().AccountFor(acct).Conversation("#general").DateFile("2026-04-26").Path()
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read date file: %v", err)
			}
			line := strings.TrimRight(string(data), "\n")

			if tt.wantKeys {
				if !strings.Contains(line, `"thread_ts":"`+tt.threadTS+`"`) {
					t.Errorf("expected thread_ts on delete line; got: %s", line)
				}
				if !strings.Contains(line, `"thread_id":"`+tt.threadTS+`"`) {
					t.Errorf("expected thread_id on delete line; got: %s", line)
				}
			} else {
				if strings.Contains(line, "thread_ts") || strings.Contains(line, "thread_id") {
					t.Errorf("non-thread delete must omit thread_ts/thread_id; got: %s", line)
				}
			}
		})
	}
}

// TestWriteThreadMessage_OmitemptyOnDisk verifies parent records emit
// neither thread_ts nor thread_id (omitempty), and reply records emit
// both keys with the parent's TS as their value — the on-disk shape that
// makes lines greppable from either vocabulary.
func TestWriteThreadMessage_OmitemptyOnDisk(t *testing.T) {
	ms, _, acct := newTestMessageStore(t)
	rs := ResolvedSender{ChannelName: "#general", SenderName: "Alice", SenderID: "U001"}
	raw := slackraw.NewSlackRawContent(goslack.Msg{})

	if _, err := ms.WriteThreadMessage(rs, "P1", "parent",
		time.Unix(1700000001, 0), "1700000001.000001", false,
		modelv1.ViaOrganic, raw); err != nil {
		t.Fatalf("WriteThreadMessage: %v", err)
	}
	if _, err := ms.WriteThreadMessage(rs, "P1", "reply",
		time.Unix(1700000010, 0), "1700000010.000010", true,
		modelv1.ViaOrganic, raw); err != nil {
		t.Fatalf("WriteThreadMessage reply: %v", err)
	}

	// The helper sets PIGEON_DATA_DIR; DefaultDataRoot honors it.
	path := paths.DefaultDataRoot().AccountFor(acct).Conversation("#general").ThreadFile("P1").Path()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read thread file: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("thread file has %d lines, want 2:\n%s", len(lines), data)
	}
	if strings.Contains(lines[0], "thread_ts") || strings.Contains(lines[0], "thread_id") {
		t.Errorf("parent line should omit thread_ts/thread_id; got:\n%s", lines[0])
	}
	if !strings.Contains(lines[1], `"thread_ts":"P1"`) {
		t.Errorf("reply line should contain thread_ts; got:\n%s", lines[1])
	}
	if !strings.Contains(lines[1], `"thread_id":"P1"`) {
		t.Errorf("reply line should contain thread_id; got:\n%s", lines[1])
	}
}
