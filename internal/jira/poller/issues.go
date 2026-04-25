package poller

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
// Returns the number of issues processed and any error encountered. The
// cursor is advanced only when the entire batch succeeds — if any issue
// failed, cursors.Issues.UpdatedAfter stays put so the next poll retries
// the whole batch (dedup-by-id absorbs the duplicate writes).
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

	keys, err := discoverKeys(ctx, c, project, ver, cursor)
	if err != nil {
		return 0, fmt.Errorf("discover keys: %w", err)
	}
	if len(keys) == 0 {
		return 0, nil
	}

	n, maxUpdated, err := fetchAndWrite(ctx, c, s, projDir, ver, keys)
	if err == nil && maxUpdated > cursor {
		cursors.Issues.UpdatedAfter = maxUpdated
	}
	return n, err
}

// discoverKeys returns the issue keys whose snapshots need refreshing.
// First run (cursor empty) → backfill: active + closed-90d.
// Subsequent runs → incremental: project + updated > cursor.
func discoverKeys(ctx context.Context, c *jira.Client, project string, ver APIVersion, cursor string) ([]string, error) {
	if cursor == "" {
		return backfillKeys(ctx, c, project, ver)
	}
	return incrementalKeys(ctx, c, project, ver, cursor)
}

func backfillKeys(ctx context.Context, c *jira.Client, project string, ver APIVersion) ([]string, error) {
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

func incrementalKeys(ctx context.Context, c *jira.Client, project string, ver APIVersion, cursor string) ([]string, error) {
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

// fetchAndWrite pulls each key's full body, splits it into issue + comment
// lines, and appends them to the per-issue file. Returns the count, the
// max fields.updated seen (for cursor advance), and any error encountered
// along the way.
//
// Per-issue errors are accumulated; the loop continues so that a single
// 401/404 on one issue doesn't block progress on the others. The combined
// error is returned at the end so the caller can decide whether to advance
// the cursor.
func fetchAndWrite(
	ctx context.Context,
	c *jira.Client,
	s *store.FSStore,
	projDir paths.JiraProjectDir,
	ver APIVersion,
	keys []string,
) (int, string, error) {
	var errs []error
	var maxUpdated string

	for _, key := range keys {
		if ctx.Err() != nil {
			return 0, "", ctx.Err()
		}

		raw, err := getIssueRaw(c, key, ver)
		if err != nil {
			errs = append(errs, fmt.Errorf("fetch %s: %w", key, err))
			continue
		}

		issueLine, commentLines, updated, err := splitIssueRaw(key, []byte(raw))
		if err != nil {
			errs = append(errs, fmt.Errorf("parse %s: %w", key, err))
			continue
		}

		file := projDir.IssueFile(key)
		if err := s.AppendLine(file, issueLine); err != nil {
			errs = append(errs, fmt.Errorf("write issue %s: %w", key, err))
			continue
		}
		for _, cl := range commentLines {
			if err := s.AppendLine(file, cl); err != nil {
				errs = append(errs, fmt.Errorf("write comment for %s: %w", key, err))
			}
		}

		if updated > maxUpdated {
			maxUpdated = updated
		}
	}

	return len(keys), maxUpdated, errors.Join(errs...)
}
