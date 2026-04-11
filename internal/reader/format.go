package reader

import (
	"fmt"
	"strings"
	"time"

	"github.com/anish749/pigeon/internal/store/modelv1"
)

// FormatGmail formats Gmail results for terminal output.
func FormatGmail(result *GmailResult) string {
	if len(result.Emails) == 0 {
		return "No emails found."
	}

	var b strings.Builder
	for i, e := range result.Emails {
		if i > 0 {
			b.WriteString("\n")
		}
		ts := e.Ts.In(time.Local).Format("Jan 02 15:04")
		from := e.FromName
		if from == "" {
			from = e.From
		}

		fmt.Fprintf(&b, "[%s] %s — %s\n", ts, from, e.Subject)
		if e.Snippet != "" {
			fmt.Fprintf(&b, "  %s\n", truncate(e.Snippet, 120))
		}
	}
	return b.String()
}

// FormatGmailFull formats a single email with full body.
func FormatGmailFull(e *modelv1.EmailLine) string {
	var b strings.Builder
	ts := e.Ts.In(time.Local).Format("Mon Jan 02 15:04 MST")
	fmt.Fprintf(&b, "From: %s <%s>\n", e.FromName, e.From)
	fmt.Fprintf(&b, "To: %s\n", strings.Join(e.To, ", "))
	if len(e.CC) > 0 {
		fmt.Fprintf(&b, "CC: %s\n", strings.Join(e.CC, ", "))
	}
	fmt.Fprintf(&b, "Date: %s\n", ts)
	fmt.Fprintf(&b, "Subject: %s\n", e.Subject)
	if len(e.Labels) > 0 {
		fmt.Fprintf(&b, "Labels: %s\n", strings.Join(e.Labels, ", "))
	}
	if len(e.Attach) > 0 {
		var names []string
		for _, a := range e.Attach {
			names = append(names, a.Name)
		}
		fmt.Fprintf(&b, "Attachments: %s\n", strings.Join(names, ", "))
	}
	b.WriteString("\n")
	b.WriteString(e.Text)
	return b.String()
}

// FormatCalendar formats Calendar results for terminal output.
func FormatCalendar(result *CalendarResult) string {
	if len(result.Events) == 0 {
		return "No events found."
	}

	var b strings.Builder
	var lastDate string
	for _, e := range result.Events {
		start := eventStartTime(e)
		date := start.In(time.Local).Format("Mon Jan 02")
		if date != lastDate {
			if lastDate != "" {
				b.WriteString("\n")
			}
			fmt.Fprintf(&b, "── %s ──\n", date)
			lastDate = date
		}

		startStr := start.In(time.Local).Format("15:04")
		endStr := ""
		if e.Runtime.End != nil {
			if e.Runtime.End.DateTime != "" {
				if t, err := time.Parse(time.RFC3339, e.Runtime.End.DateTime); err == nil {
					endStr = t.In(time.Local).Format("15:04")
				}
			}
		}

		if e.Runtime.Start != nil && e.Runtime.Start.Date != "" && e.Runtime.Start.DateTime == "" {
			// All-day event.
			fmt.Fprintf(&b, "  all-day  %s\n", e.Runtime.Summary)
		} else if endStr != "" {
			fmt.Fprintf(&b, "  %s–%s  %s\n", startStr, endStr, e.Runtime.Summary)
		} else {
			fmt.Fprintf(&b, "  %s       %s\n", startStr, e.Runtime.Summary)
		}

		if e.Runtime.Location != "" {
			fmt.Fprintf(&b, "             📍 %s\n", e.Runtime.Location)
		}
		if e.Runtime.HangoutLink != "" {
			fmt.Fprintf(&b, "             🔗 %s\n", e.Runtime.HangoutLink)
		}
	}
	return b.String()
}

// FormatDrive formats Drive results for terminal output.
func FormatDrive(result *DriveResult) string {
	var b strings.Builder

	fmt.Fprintf(&b, "── %s ──\n\n", result.Title)

	for _, tab := range result.Tabs {
		if len(result.Tabs) > 1 {
			fmt.Fprintf(&b, "─── %s ───\n\n", tab.Name)
		}
		b.WriteString(tab.Content)
		if !strings.HasSuffix(tab.Content, "\n") {
			b.WriteString("\n")
		}
	}

	if len(result.Comments) > 0 {
		b.WriteString("\n── Comments ──\n\n")
		for _, c := range result.Comments {
			author := c.Runtime.Author.DisplayName
			resolved := ""
			if c.Runtime.Resolved {
				resolved = " ✓"
			}
			anchor := ""
			if c.Runtime.QuotedFileContent != nil && c.Runtime.QuotedFileContent.Value != "" {
				anchor = fmt.Sprintf(" on %q", truncate(c.Runtime.QuotedFileContent.Value, 40))
			}

			fmt.Fprintf(&b, "  %s%s%s: %s\n", author, anchor, resolved, c.Runtime.Content)

			for _, r := range c.Runtime.Replies {
				fmt.Fprintf(&b, "    ↳ %s: %s\n", r.Author.DisplayName, r.Content)
			}
		}
	}

	return b.String()
}

// FormatLinearIssue formats a single Linear issue result.
func FormatLinearIssue(result *LinearIssueResult) string {
	var b strings.Builder
	iss := result.Issue

	state := "unknown"
	if iss.State != nil {
		state = iss.State.Name
	}
	assignee := "unassigned"
	if iss.Assignee != nil {
		assignee = iss.Assignee.DisplayName
		if assignee == "" {
			assignee = iss.Assignee.Name
		}
	}

	fmt.Fprintf(&b, "%s — %s\n", iss.Identifier, iss.Title)
	fmt.Fprintf(&b, "  State: %s  Assignee: %s\n", state, assignee)

	if len(result.Comments) > 0 {
		b.WriteString("\n── Comments ──\n\n")
		for _, c := range result.Comments {
			author := "unknown"
			if c.User != nil {
				author = c.User.DisplayName
				if author == "" {
					author = c.User.Name
				}
			}
			ts := ""
			if t, err := time.Parse(time.RFC3339, c.CreatedAt); err == nil {
				ts = t.In(time.Local).Format("Jan 02 15:04")
			}
			indent := "  "
			if c.ParentID != "" {
				indent = "    ↳ "
			}
			fmt.Fprintf(&b, "%s[%s] %s: %s\n", indent, ts, author, truncateMultiline(c.Body, 200))
		}
	}

	return b.String()
}

// FormatLinearList formats a list of Linear issues.
func FormatLinearList(result *LinearListResult) string {
	if len(result.Issues) == 0 {
		return "No issues found."
	}

	var b strings.Builder
	for _, iss := range result.Issues {
		state := ""
		if iss.State != nil {
			state = iss.State.Name
		}
		assignee := ""
		if iss.Assignee != nil {
			assignee = iss.Assignee.DisplayName
			if assignee == "" {
				assignee = iss.Assignee.Name
			}
		}

		fmt.Fprintf(&b, "  %-12s %-14s %-12s %s\n", iss.Identifier, state, assignee, truncate(iss.Title, 50))
	}
	return b.String()
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
}

func truncateMultiline(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	return truncate(s, maxLen)
}
