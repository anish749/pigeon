package paths

import "testing"

func acctDir() AccountDir {
	return NewDataRoot("/tmp/test").Platform("gws").AccountFromSlug("user-at-gmail-com")
}

func TestGmailDir_Path(t *testing.T) {
	got := acctDir().Gmail().Path()
	want := "/tmp/test/gws/user-at-gmail-com/gmail"
	if got != want {
		t.Errorf("Gmail().Path() = %q, want %q", got, want)
	}
}

func TestGmailDir_DateFile(t *testing.T) {
	got := acctDir().Gmail().DateFile("2024-01-15").Path()
	want := "/tmp/test/gws/user-at-gmail-com/gmail/2024-01-15.jsonl"
	if got != want {
		t.Errorf("Gmail().DateFile() = %q, want %q", got, want)
	}
}

func TestCalendarDir_Path(t *testing.T) {
	got := acctDir().Calendar("primary").Path()
	want := "/tmp/test/gws/user-at-gmail-com/gcalendar/primary"
	if got != want {
		t.Errorf("Calendar().Path() = %q, want %q", got, want)
	}
}

func TestCalendarDir_DateFile(t *testing.T) {
	got := acctDir().Calendar("primary").DateFile("2024-03-20").Path()
	want := "/tmp/test/gws/user-at-gmail-com/gcalendar/primary/2024-03-20.jsonl"
	if got != want {
		t.Errorf("Calendar().DateFile() = %q, want %q", got, want)
	}
}

func TestDriveDir_Path(t *testing.T) {
	got := acctDir().Drive().Path()
	want := "/tmp/test/gws/user-at-gmail-com/gdrive"
	if got != want {
		t.Errorf("Drive().Path() = %q, want %q", got, want)
	}
}

func TestDriveFileDir_Path(t *testing.T) {
	got := acctDir().Drive().File("meeting-notes-abc12345").Path()
	want := "/tmp/test/gws/user-at-gmail-com/gdrive/meeting-notes-abc12345"
	if got != want {
		t.Errorf("DriveFile().Path() = %q, want %q", got, want)
	}
}

func TestDriveFileDir_MetaFile(t *testing.T) {
	got := acctDir().Drive().File("doc-abc").MetaFile().Path()
	want := "/tmp/test/gws/user-at-gmail-com/gdrive/doc-abc/meta.json"
	if got != want {
		t.Errorf("MetaFile() = %q, want %q", got, want)
	}
}

func TestDriveFileDir_CommentsFile(t *testing.T) {
	got := acctDir().Drive().File("doc-abc").CommentsFile().Path()
	want := "/tmp/test/gws/user-at-gmail-com/gdrive/doc-abc/comments.jsonl"
	if got != want {
		t.Errorf("CommentsFile() = %q, want %q", got, want)
	}
}

func TestDriveFileDir_TabFile(t *testing.T) {
	got := acctDir().Drive().File("doc-abc").TabFile("Introduction")
	want := "/tmp/test/gws/user-at-gmail-com/gdrive/doc-abc/Introduction.md"
	if got != want {
		t.Errorf("TabFile() = %q, want %q", got, want)
	}
}

func TestDriveFileDir_SheetFile(t *testing.T) {
	got := acctDir().Drive().File("sheet-xyz").SheetFile("Revenue")
	want := "/tmp/test/gws/user-at-gmail-com/gdrive/sheet-xyz/Revenue.csv"
	if got != want {
		t.Errorf("SheetFile() = %q, want %q", got, want)
	}
}

func TestDriveFileDir_FormulaFile(t *testing.T) {
	got := acctDir().Drive().File("sheet-xyz").FormulaFile("Revenue")
	want := "/tmp/test/gws/user-at-gmail-com/gdrive/sheet-xyz/Revenue.formulas.csv"
	if got != want {
		t.Errorf("FormulaFile() = %q, want %q", got, want)
	}
}

func TestDriveFileDir_AttachmentFile(t *testing.T) {
	got := acctDir().Drive().File("doc-abc").AttachmentFile("image.png")
	want := "/tmp/test/gws/user-at-gmail-com/gdrive/doc-abc/attachments/image.png"
	if got != want {
		t.Errorf("AttachmentFile() = %q, want %q", got, want)
	}
}
