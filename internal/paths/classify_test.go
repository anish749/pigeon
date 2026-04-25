package paths

import (
	"fmt"
	"testing"

	"github.com/anish749/pigeon/internal/account"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		name string
		path string
		want DataFile
	}{
		// Messaging.
		{
			name: "messaging date file",
			path: "/data/slack/acme/#general/2026-04-07.jsonl",
			want: MessagingDateFile("/data/slack/acme/#general/2026-04-07.jsonl"),
		},
		{
			name: "whatsapp date file",
			path: "/data/whatsapp/+1234567890/+9876543210/2026-04-07.jsonl",
			want: MessagingDateFile("/data/whatsapp/+1234567890/+9876543210/2026-04-07.jsonl"),
		},
		{
			name: "thread file",
			path: "/data/slack/acme/#general/threads/1742100000.123456.jsonl",
			want: ThreadFile("/data/slack/acme/#general/threads/1742100000.123456.jsonl"),
		},
		{
			name: "conversation literally named threads keeps date file",
			path: "/data/slack/acme/threads/2026-04-07.jsonl",
			want: MessagingDateFile("/data/slack/acme/threads/2026-04-07.jsonl"),
		},
		{
			name: "conversation meta sidecar",
			path: "/data/slack/acme/#general/.meta.json",
			want: ConvMetaFile("/data/slack/acme/#general/.meta.json"),
		},

		// Account-level state files.
		{
			name: "maintenance state",
			path: "/data/slack/acme/.maintenance.json",
			want: MaintenanceFile("/data/slack/acme/.maintenance.json"),
		},
		{
			name: "sync cursors yaml",
			path: "/data/gws/anish/.sync-cursors.yaml",
			want: SyncCursorsFile("/data/gws/anish/.sync-cursors.yaml"),
		},
		{
			name: "poll metrics jsonl",
			path: "/data/gws/anish/.poll-metrics.jsonl",
			want: PollMetricsFile("/data/gws/anish/.poll-metrics.jsonl"),
		},

		// Identity.
		{
			name: "people identity file",
			path: "/data/slack/acme/identity/people.jsonl",
			want: PeopleFile("/data/slack/acme/identity/people.jsonl"),
		},

		// Email.
		{
			name: "gmail date file",
			path: "/data/gws/anish/gmail/2026-04-07.jsonl",
			want: EmailDateFile("/data/gws/anish/gmail/2026-04-07.jsonl"),
		},
		{
			name: "pending email deletes",
			path: "/data/gws/anish/gmail/.pending-email-deletes",
			want: PendingDeletesFile("/data/gws/anish/gmail/.pending-email-deletes"),
		},

		// Calendar.
		{
			name: "calendar date file primary",
			path: "/data/gws/anish/gcalendar/primary/2026-04-07.jsonl",
			want: CalendarDateFile("/data/gws/anish/gcalendar/primary/2026-04-07.jsonl"),
		},
		{
			name: "calendar date file named id",
			path: "/data/gws/anish/gcalendar/team@x.com/2026-04-07.jsonl",
			want: CalendarDateFile("/data/gws/anish/gcalendar/team@x.com/2026-04-07.jsonl"),
		},

		// Drive content.
		{
			name: "drive markdown tab",
			path: "/data/gws/anish/gdrive/doc-abc/Notes.md",
			want: TabFile("/data/gws/anish/gdrive/doc-abc/Notes.md"),
		},
		{
			name: "drive sheet csv",
			path: "/data/gws/anish/gdrive/sheet-xyz/Q1.csv",
			want: SheetFile("/data/gws/anish/gdrive/sheet-xyz/Q1.csv"),
		},
		{
			name: "drive formula csv preferred over generic csv",
			path: "/data/gws/anish/gdrive/sheet-xyz/Q1.formulas.csv",
			want: FormulaFile("/data/gws/anish/gdrive/sheet-xyz/Q1.formulas.csv"),
		},
		{
			name: "drive comments jsonl",
			path: "/data/gws/anish/gdrive/doc-abc/comments.jsonl",
			want: CommentsFile("/data/gws/anish/gdrive/doc-abc/comments.jsonl"),
		},
		{
			name: "drive attachment image",
			path: "/data/gws/anish/gdrive/doc-abc/attachments/img1.png",
			want: AttachmentFile("/data/gws/anish/gdrive/doc-abc/attachments/img1.png"),
		},
		{
			name: "drive meta sidecar",
			path: "/data/gws/anish/gdrive/doc-abc/drive-meta-2026-04-07.json",
			want: DriveMetaFile{dir: "/data/gws/anish/gdrive/doc-abc", name: "drive-meta-2026-04-07.json"},
		},

		// Linear.
		{
			name: "linear issue file",
			path: "/data/linear-issues/acme/issues/PROJ-123.jsonl",
			want: IssueFile("/data/linear-issues/acme/issues/PROJ-123.jsonl"),
		},

		// Workstream router state.
		{
			name: "workstream list",
			path: "/data/.workspaces/acme/workstream/workstreams.json",
			want: WorkstreamsFile("/data/.workspaces/acme/workstream/workstreams.json"),
		},
		{
			name: "workstream proposals",
			path: "/data/.workspaces/acme/workstream/proposals.json",
			want: WorkstreamProposalsFile("/data/.workspaces/acme/workstream/proposals.json"),
		},
		{
			name: "workstreams.json outside workstream subdir is not classified",
			path: "/data/.workspaces/acme/affinityrouter/workstreams.json",
			want: nil,
		},

		// Negative cases.
		{
			name: "drive meta with malformed date returns nil",
			path: "/data/gws/anish/gdrive/doc-abc/drive-meta-not-a-date.json",
			want: nil,
		},
		{
			name: "stray file outside known shape returns nil",
			path: "/data/gws/anish/something-random.txt",
			want: nil,
		},
		{
			name: "issues subdir under non-linear platform is not a Linear issue",
			path: "/data/slack/acme/issues/PROJ-123.jsonl",
			want: nil,
		},
		{
			name: "conversation literally named issues keeps date children as messaging",
			path: "/data/slack/acme/issues/2026-04-07.jsonl",
			want: MessagingDateFile("/data/slack/acme/issues/2026-04-07.jsonl"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Classify(tt.path)
			if got != tt.want {
				t.Errorf("Classify(%q) = %#v, want %#v", tt.path, got, tt.want)
			}
		})
	}
}

// TestClassify_RoundTripsConstructors verifies that paths produced by the
// typed constructors classify back to the same type. This is the contract
// the registry leans on: the type carried by a value going out of paths is
// the same type Classify returns when handed its string.
func TestClassify_RoundTripsConstructors(t *testing.T) {
	root := NewDataRoot("/data")
	slackAcct := root.AccountFor(account.New("slack", "acme"))
	gwsAcct := root.AccountFor(account.New("gws", "anish"))

	cases := []struct {
		name string
		got  DataFile
	}{
		{"MessagingDateFile", slackAcct.Conversation("#general").DateFile("2026-04-07")},
		{"ThreadFile", slackAcct.Conversation("#general").ThreadFile("1742100000.123456")},
		{"ConvMetaFile", slackAcct.Conversation("#general").MetaFile()},
		{"MaintenanceFile", slackAcct.MaintenanceFile()},
		{"SyncCursorsFile", slackAcct.SyncCursorsFile()},
		{"PollMetricsFile", gwsAcct.PollMetricsFile()},
		{"PeopleFile", slackAcct.Identity().PeopleFile()},
		{"EmailDateFile", gwsAcct.Gmail().DateFile("2026-04-07")},
		{"PendingDeletesFile", gwsAcct.Gmail().PendingDeletesFile()},
		{"CalendarDateFile", gwsAcct.Calendar("primary").DateFile("2026-04-07")},
		{"IssueFile", root.AccountFor(account.New("linear-issues", "acme")).Linear().IssueFile("PROJ-123")},
		{"WorkstreamsFile", root.Workspace("acme").WorkstreamStore().WorkstreamsFile()},
		{"WorkstreamProposalsFile", root.Workspace("acme").WorkstreamStore().ProposalsFile()},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			classified := Classify(tt.got.Path())
			if classified == nil {
				t.Fatalf("Classify(%q) = nil, want %T", tt.got.Path(), tt.got)
			}
			if classified.Path() != tt.got.Path() {
				t.Errorf("Classify(%q).Path() = %q, want %q", tt.got.Path(), classified.Path(), tt.got.Path())
			}
			// Concrete type identity check.
			if got, want := fmt.Sprintf("%T", classified), fmt.Sprintf("%T", tt.got); got != want {
				t.Errorf("Classify(%q) type = %s, want %s", tt.got.Path(), got, want)
			}
		})
	}
}
