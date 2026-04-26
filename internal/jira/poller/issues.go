package poller

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	jira "github.com/ankitpokhrel/jira-cli/pkg/jira"

	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
)

// backfillDays is the look-back window for recently closed issues during
// first-run backfill. Matches Linear's 90-day depth.
const backfillDays = 90

// PollIssues runs one sync cycle for a single Jira project. On first run
// (no cursor) it backfills active + recently-closed issues; on subsequent
// runs it queries `updated > <cursor>` for the project. For each issue
// key returned, it fetches the full body via GetIssueRaw and writes one
// jira-issue line plus N jira-comment lines.
//
// Returns the number of issues successfully written and any error
// encountered. The cursor is advanced only when there are no real
// (non-404) errors — a single 500 or network failure keeps the cursor
// put so the next poll retries. 404s are treated as tombstones (issue
// deleted or token lost access between discovery and fetch); the cursor
// advances past them via their discovery-time `updated` so the all-404
// case doesn't stall ingest.
func PollIssues(
	ctx context.Context,
	c *jira.Client,
	s *store.FSStore,
	projDir paths.JiraProjectDir,
	project string,
	ver APIVersion,
	cursors *store.JiraCursors,
) (int, error) {
	cursor := cursors.Issues.UpdatedAfter

	refs, err := discoverKeys(ctx, c, project, ver, cursor)
	if err != nil {
		return 0, fmt.Errorf("discover keys: %w", err)
	}
	if len(refs) == 0 {
		return 0, nil
	}

	written, maxUpdated, err := fetchAndWrite(ctx, c, s, projDir, ver, refs)
	if err == nil && maxUpdated > cursor {
		cursors.Issues.UpdatedAfter = maxUpdated
	}
	return written, err
}

// discoverKeys returns issue references whose snapshots need refreshing.
// First run (cursor empty) → backfill: active + closed-90d.
// Subsequent runs → incremental: project + updated > cursor.
func discoverKeys(ctx context.Context, c *jira.Client, project string, ver APIVersion, cursor string) ([]issueRef, error) {
	if cursor == "" {
		return backfillKeys(ctx, c, project, ver)
	}
	return incrementalKeys(ctx, c, project, ver, cursor)
}

func backfillKeys(ctx context.Context, c *jira.Client, project string, ver APIVersion) ([]issueRef, error) {
	slog.Info("jira backfill starting", "project", project)

	activeJQL := fmt.Sprintf(`project = %s AND statusCategory != Done ORDER BY updated DESC`, jqlEscape(project))
	active, err := searchKeys(ctx, c, activeJQL, ver)
	if err != nil {
		return nil, fmt.Errorf("active: %w", err)
	}

	cutoff := time.Now().UTC().AddDate(0, 0, -backfillDays).Format(jqlDateLayout)
	closedJQL := fmt.Sprintf(`project = %s AND statusCategory = Done AND updated > %q ORDER BY updated DESC`,
		jqlEscape(project), cutoff)
	closed, err := searchKeys(ctx, c, closedJQL, ver)
	if err != nil {
		return nil, fmt.Errorf("closed: %w", err)
	}

	slog.Info("jira backfill fetched",
		"project", project, "active", len(active), "closed", len(closed))
	return append(active, closed...), nil
}

func incrementalKeys(ctx context.Context, c *jira.Client, project string, ver APIVersion, cursor string) ([]issueRef, error) {
	cutoff, err := jqlCutoff(cursor)
	if err != nil {
		return nil, fmt.Errorf("invalid cursor: %w", err)
	}
	// JQL silently returns zero matches on bad date format. jqlCutoff
	// guarantees the format is valid; if it ever didn't, this query would
	// look exactly like "no activity" with no error.
	jql := fmt.Sprintf(`project = %s AND updated > %q ORDER BY updated ASC`,
		jqlEscape(project), cutoff)
	return searchKeys(ctx, c, jql, ver)
}

// is404 reports whether err comes from an HTTP 404 returned by pkg/jira.
// 404 means the issue was deleted or the token lost access between the
// discovery search and the per-issue raw fetch. Retrying won't recover
// the data, so the poller treats 404 as a tombstone and lets the cursor
// advance past it on the discovery-time `updated`.
func is404(err error) bool {
	var unexpected *jira.ErrUnexpectedResponse
	if !errors.As(err, &unexpected) {
		return false
	}
	return unexpected.StatusCode == http.StatusNotFound
}

// fetchAndWrite pulls each ref's full body, splits it into issue + comment
// lines, and appends them to the per-issue file. Returns the number of
// issues actually written, the max `updated` to advance the cursor to,
// and any non-404 error encountered along the way.
//
// Cursor advance: maxUpdated is the larger of the discovery-time
// `updated` for every PROCESSED ref (successfully written or 404'd).
// Real failures (non-404) do NOT contribute to maxUpdated, but the
// returned non-nil error blocks the caller from advancing the cursor at
// all — so they get retried next poll. 404s contribute to maxUpdated
// without contributing to errs, so an all-404 batch still progresses
// the cursor.
func fetchAndWrite(
	ctx context.Context,
	c *jira.Client,
	s *store.FSStore,
	projDir paths.JiraProjectDir,
	ver APIVersion,
	refs []issueRef,
) (int, string, error) {
	var errs []error
	var maxUpdated string
	var written, skipped404 int

	for _, ref := range refs {
		if ctx.Err() != nil {
			return written, maxUpdated, ctx.Err()
		}

		raw, err := getIssueRaw(c, ref.Key, ver)
		if err != nil {
			if is404(err) {
				slog.Warn("jira issue not found, advancing cursor past it",
					"issue", ref.Key, "updated", ref.Updated)
				if ref.Updated > maxUpdated {
					maxUpdated = ref.Updated
				}
				skipped404++
				continue
			}
			errs = append(errs, fmt.Errorf("fetch %s: %w", ref.Key, err))
			continue
		}

		issueLine, commentLines, updated, err := splitIssueRaw(ref.Key, []byte(raw))
		if err != nil {
			errs = append(errs, fmt.Errorf("parse %s: %w", ref.Key, err))
			continue
		}

		file := projDir.IssueFile(ref.Key)
		if err := s.AppendLine(file, issueLine); err != nil {
			errs = append(errs, fmt.Errorf("write issue %s: %w", ref.Key, err))
			continue
		}
		for _, cl := range commentLines {
			if err := s.AppendLine(file, cl); err != nil {
				errs = append(errs, fmt.Errorf("write comment for %s: %w", ref.Key, err))
			}
		}

		written++
		if updated > maxUpdated {
			maxUpdated = updated
		}
	}

	if skipped404 > 0 || len(errs) > 0 {
		slog.Info("jira fetch summary",
			"project_dir", projDir.Path(),
			"discovered", len(refs),
			"written", written,
			"skipped_404", skipped404,
			"errors", len(errs))
	}
	return written, maxUpdated, errors.Join(errs...)
}
