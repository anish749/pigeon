package slack

import (
	"os"
	"strings"
	"testing"
	"time"

	goslack "github.com/slack-go/slack"

	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
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

// TestAppendEditDelete_ThreadRouting verifies that edits/deletes targeting
// a thread reply land in the thread file (so CompactThread can reconcile
// them on read) and that top-level edits/deletes still land in the date
// file. The line itself carries thread_ts/thread_id when populated;
// top-level lines emit neither key.
func TestAppendEditDelete_ThreadRouting(t *testing.T) {
	const threadTS = "1700000001.000001"

	tests := []struct {
		name     string
		kind     string // "edit" | "delete"
		threadTS string
		wantFile string // "thread" | "date"
		wantKeys bool
	}{
		{name: "thread reply edit lands in thread file", kind: "edit", threadTS: threadTS, wantFile: "thread", wantKeys: true},
		{name: "top-level edit lands in date file", kind: "edit", threadTS: "", wantFile: "date", wantKeys: false},
		{name: "thread reply delete lands in thread file", kind: "delete", threadTS: threadTS, wantFile: "thread", wantKeys: true},
		{name: "top-level delete lands in date file", kind: "delete", threadTS: "", wantFile: "date", wantKeys: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ms, _, acct := newTestMessageStore(t)
			rs := ResolvedSender{ChannelName: "#general", SenderName: "Alice", SenderID: "U001"}
			when := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)

			switch tt.kind {
			case "edit":
				if _, err := ms.AppendEdit(rs, "1700000010.000010", tt.threadTS,
					"new text", when, slackraw.NewSlackRawContent(goslack.Msg{})); err != nil {
					t.Fatalf("AppendEdit: %v", err)
				}
			case "delete":
				if _, err := ms.AppendDelete(rs, "1700000010.000010", tt.threadTS, when); err != nil {
					t.Fatalf("AppendDelete: %v", err)
				}
			}

			conv := paths.DefaultDataRoot().AccountFor(acct).Conversation("#general")
			var wantPath, oppositePath string
			switch tt.wantFile {
			case "thread":
				wantPath = conv.ThreadFile(threadTS).Path()
				oppositePath = conv.DateFile("2026-04-26").Path()
			case "date":
				wantPath = conv.DateFile("2026-04-26").Path()
				oppositePath = conv.ThreadFile(threadTS).Path()
			}

			data, err := os.ReadFile(wantPath)
			if err != nil {
				t.Fatalf("read expected file %s: %v", wantPath, err)
			}
			line := strings.TrimRight(string(data), "\n")

			if _, err := os.Stat(oppositePath); err == nil {
				t.Errorf("event also leaked into %s", oppositePath)
			}

			if tt.wantKeys {
				if !strings.Contains(line, `"thread_ts":"`+tt.threadTS+`"`) {
					t.Errorf("expected thread_ts on line; got: %s", line)
				}
				if !strings.Contains(line, `"thread_id":"`+tt.threadTS+`"`) {
					t.Errorf("expected thread_id on line; got: %s", line)
				}
			} else {
				if strings.Contains(line, "thread_ts") || strings.Contains(line, "thread_id") {
					t.Errorf("non-thread line must omit thread_ts/thread_id; got: %s", line)
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

// TestThreadEditDelete_RoundTripThroughCompactThread is the end-to-end
// regression for the original bug: an edit on a thread reply must reach
// the thread compaction and rewrite the reply's text on read; a delete
// must remove the reply. AppendEdit/AppendDelete now route the line into
// the thread file, ParseThreadFile slots it into f.Edits/f.Deletes, and
// CompactThread reconciles it onto the matching reply.
func TestThreadEditDelete_RoundTripThroughCompactThread(t *testing.T) {
	const (
		channel    = "#general"
		threadTS   = "1700000001.000001"
		replyMsgID = "1700000010.000010"
	)

	t.Run("edit on thread reply rewrites text on read", func(t *testing.T) {
		ms, s, acct := newTestMessageStore(t)
		rs := ResolvedSender{ChannelName: channel, SenderName: "Alice", SenderID: "U001"}
		raw := slackraw.NewSlackRawContent(goslack.Msg{})

		if _, err := ms.WriteThreadMessage(rs, threadTS, "thread root",
			time.Unix(1700000001, 0), threadTS, false, modelv1.ViaOrganic, raw); err != nil {
			t.Fatalf("WriteThreadMessage parent: %v", err)
		}
		if _, err := ms.WriteThreadMessage(rs, threadTS, "original text",
			time.Unix(1700000010, 0), replyMsgID, true, modelv1.ViaOrganic, raw); err != nil {
			t.Fatalf("WriteThreadMessage reply: %v", err)
		}
		if _, err := ms.AppendEdit(rs, replyMsgID, threadTS, "edited text",
			time.Unix(1700000020, 0), raw); err != nil {
			t.Fatalf("AppendEdit: %v", err)
		}

		tf, err := s.ReadThread(acct, channel, threadTS)
		if err != nil {
			t.Fatalf("ReadThread: %v", err)
		}
		if tf == nil || len(tf.Replies) != 1 {
			t.Fatalf("replies = %v, want 1 reply", tf)
		}
		if got := tf.Replies[0].Text; got != "edited text" {
			t.Errorf("reply text = %q, want %q (edit lost in compaction)", got, "edited text")
		}
	})

	t.Run("delete on thread reply removes the reply on read", func(t *testing.T) {
		ms, s, acct := newTestMessageStore(t)
		rs := ResolvedSender{ChannelName: channel, SenderName: "Alice", SenderID: "U001"}
		raw := slackraw.NewSlackRawContent(goslack.Msg{})

		if _, err := ms.WriteThreadMessage(rs, threadTS, "thread root",
			time.Unix(1700000001, 0), threadTS, false, modelv1.ViaOrganic, raw); err != nil {
			t.Fatalf("WriteThreadMessage parent: %v", err)
		}
		if _, err := ms.WriteThreadMessage(rs, threadTS, "doomed reply",
			time.Unix(1700000010, 0), replyMsgID, true, modelv1.ViaOrganic, raw); err != nil {
			t.Fatalf("WriteThreadMessage reply: %v", err)
		}
		if _, err := ms.AppendDelete(rs, replyMsgID, threadTS, time.Unix(1700000020, 0)); err != nil {
			t.Fatalf("AppendDelete: %v", err)
		}

		tf, err := s.ReadThread(acct, channel, threadTS)
		if err != nil {
			t.Fatalf("ReadThread: %v", err)
		}
		if tf == nil {
			t.Fatal("ReadThread returned nil")
		}
		if len(tf.Replies) != 0 {
			t.Errorf("replies = %d, want 0 (delete should remove the reply)", len(tf.Replies))
		}
	})
}

// TestAppendReaction_ThreadRouting verifies that AppendReaction lands in
// the thread file (and stamps the line with thread tags) when threadTS is
// non-empty, and in the date file otherwise. Asserts the user-visible
// behaviour through resolved reads:
//
//   - Reaction on a thread reply attaches to the resolved reply.
//   - Reaction on a top-level message attaches to the resolved message.
//   - The thread-targeted line carries ThreadTS and ThreadID; the
//     top-level line carries neither.
//
// Pre-fix, all reactions landed in the date file and CompactThread
// dropped reactions on thread replies because it never saw them.
func TestAppendReaction_ThreadRouting(t *testing.T) {
	rs := ResolvedSender{ChannelName: "#general", SenderName: "Alice", SenderID: "U001"}
	raw := slackraw.NewSlackRawContent(goslack.Msg{})
	threadTS := "1700000001.000001"
	replyMsgID := "1700000010.000010"
	topLevelID := "1700000020.000020"
	emoji := "thumbsup"

	tests := []struct {
		name string
		// targetTS is the ID of the message being reacted to. The fixture
		// builds two distinct messages: the reply lives in the thread
		// file, the top-level lives in the date file.
		targetTS string
		// threadTS is the value AppendReaction is called with — empty
		// means "treat the target as date-file-resident".
		threadTS string
	}{
		{name: "reaction on thread reply", targetTS: replyMsgID, threadTS: threadTS},
		{name: "reaction on top-level message", targetTS: topLevelID, threadTS: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ms, s, acct := newTestMessageStore(t)
			// Fixture: a thread (parent + reply) plus a top-level message.
			if _, err := ms.WriteThreadMessage(rs, threadTS, "root",
				time.Unix(1700000001, 0), threadTS, false, modelv1.ViaOrganic, raw); err != nil {
				t.Fatalf("WriteThreadMessage parent: %v", err)
			}
			if _, err := ms.WriteThreadMessage(rs, threadTS, "the reply",
				time.Unix(1700000010, 0), replyMsgID, true, modelv1.ViaOrganic, raw); err != nil {
				t.Fatalf("WriteThreadMessage reply: %v", err)
			}
			if _, err := ms.Write(rs, "top",
				time.Unix(1700000020, 0), topLevelID, modelv1.ViaOrganic, raw); err != nil {
				t.Fatalf("Write top-level: %v", err)
			}

			react, err := ms.AppendReaction("#general", tt.targetTS, tt.threadTS,
				"Bob", "U002", emoji, false)
			if err != nil {
				t.Fatalf("AppendReaction: %v", err)
			}

			if tt.threadTS != "" {
				if react.ThreadTS != tt.threadTS || react.ThreadID != tt.threadTS {
					t.Errorf("returned ReactLine ThreadTS=%q ThreadID=%q, want both %q",
						react.ThreadTS, react.ThreadID, tt.threadTS)
				}
				tf, err := s.ReadThread(acct, "#general", threadTS)
				if err != nil {
					t.Fatalf("ReadThread: %v", err)
				}
				if tf == nil || len(tf.Replies) != 1 {
					t.Fatalf("expected 1 reply on read, got %v", tf)
				}
				got := tf.Replies[0]
				if len(got.Reactions) != 1 {
					t.Errorf("reply has %d reactions, want 1 (reaction lost in compaction)", len(got.Reactions))
				} else if got.Reactions[0].Emoji != emoji {
					t.Errorf("reaction emoji = %q, want %q", got.Reactions[0].Emoji, emoji)
				}
				return
			}

			// threadTS == "" path: the line should land in the date file
			// and resolve onto the top-level message; the reply's
			// reactions should remain empty.
			if react.ThreadTS != "" || react.ThreadID != "" {
				t.Errorf("top-level reaction should carry no thread tags; got ThreadTS=%q ThreadID=%q",
					react.ThreadTS, react.ThreadID)
			}
			df, err := s.ReadConversation(acct, "#general", store.ReadOpts{Last: 100})
			if err != nil {
				t.Fatalf("ReadConversation: %v", err)
			}
			var matched bool
			for _, m := range df.Messages {
				if m.ID == topLevelID {
					if len(m.Reactions) != 1 || m.Reactions[0].Emoji != emoji {
						t.Errorf("top-level reactions = %v, want one %q", m.Reactions, emoji)
					}
					matched = true
				}
			}
			if !matched {
				t.Errorf("top-level message %s not found in resolved date file", topLevelID)
			}
		})
	}
}
