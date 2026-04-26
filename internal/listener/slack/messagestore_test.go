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

// TestWriteThreadMessage_StampsThreadTS round-trips through the store and
// asserts the persisted JSONL line carries thread_ts only for thread
// replies, never for the parent record.
func TestWriteThreadMessage_StampsThreadTS(t *testing.T) {
	tests := []struct {
		name     string
		isReply  bool
		threadTS string
		wantTS   string // expected MsgLine.ThreadTS after read-back
	}{
		{
			name:     "reply gets thread_ts",
			isReply:  true,
			threadTS: "1700000001.000001",
			wantTS:   "1700000001.000001",
		},
		{
			name:     "parent record (isReply=false) does not get thread_ts",
			isReply:  false,
			threadTS: "1700000001.000001",
			wantTS:   "",
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
			if written.ThreadTS != tt.wantTS {
				t.Errorf("returned MsgLine.ThreadTS = %q, want %q", written.ThreadTS, tt.wantTS)
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
			if got.ThreadTS != tt.wantTS {
				t.Errorf("read-back MsgLine.ThreadTS = %q, want %q", got.ThreadTS, tt.wantTS)
			}
		})
	}
}

// TestWriteThreadMessage_OmitemptyOnDisk verifies parent records do not
// emit a thread_ts JSON field at all (omitempty), keeping the on-disk
// schema clean for non-reply lines.
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
	if strings.Contains(lines[0], "thread_ts") {
		t.Errorf("parent line should omit thread_ts; got:\n%s", lines[0])
	}
	if !strings.Contains(lines[1], `"thread_ts":"P1"`) {
		t.Errorf("reply line should contain thread_ts; got:\n%s", lines[1])
	}
}
