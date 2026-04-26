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
// returned, it fetches the full body via GetIssueRaw and writes one
// jira-issue line plus N jira-comment lines.
//
// Returns the number of issue snapshots successfully written and any
// error encountered. The cursor advances when the batch finished with
// no transient errors — 404s are treated as permanent (issue gone) and
// don't block cursor advance; their search-time `updated` is folded
// into maxUpdated so we don't re-discover them next poll.
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

	issues, err := discoverIssues(ctx, c, project, ver, cursor)
	if err != nil {
		return 0, fmt.Errorf("discover issues: %w", err)
	}
	if len(issues) == 0 {
		return 0, nil
	}

	written, maxUpdated, err := fetchAndWrite(ctx, c, s, projDir, ver, issues)
	if err == nil && maxUpdated > cursor {
		cursors.Issues.UpdatedAfter = maxUpdated
	}
	return written, err
}

// discoverIssues returns the issues whose snapshots need refreshing.
// First run (cursor empty) → backfill: active + closed-90d.
// Subsequent runs → incremental: project + updated > cursor.
func discoverIssues(ctx context.Context, c *jira.Client, project string, ver APIVersion, cursor string) ([]discoveredIssue, error) {
	if cursor == "" {
		return backfillIssues(ctx, c, project, ver)
	}
	return incrementalIssues(ctx, c, project, ver, cursor)
}

func backfillIssues(ctx context.Context, c *jira.Client, project string, ver APIVersion) ([]discoveredIssue, error) {
	slog.Info("jira backfill starting", "project", project)

	activeJQL := fmt.Sprintf(`project = %s AND statusCategory != Done ORDER BY updated DESC`, jqlEscape(project))
	active, err := searchIssues(ctx, c, activeJQL, ver)
	if err != nil {
		return nil, fmt.Errorf("active: %w", err)
	}

	cutoff := time.Now().UTC().AddDate(0, 0, -backfillDays).Format(jqlDateLayout)
	closedJQL := fmt.Sprintf(`project = %s AND statusCategory = Done AND updated > %q ORDER BY updated DESC`,
		jqlEscape(project), cutoff)
	closed, err := searchIssues(ctx, c, closedJQL, ver)
	if err != nil {
		return nil, fmt.Errorf("closed: %w", err)
	}

	slog.Info("jira backfill fetched",
		"project", project, "active", len(active), "closed", len(closed))
	return append(active, closed...), nil
}

func incrementalIssues(ctx context.Context, c *jira.Client, project string, ver APIVersion, cursor string) ([]discoveredIssue, error) {
	cutoff, err := jqlCutoff(cursor)
	if err != nil {
		return nil, fmt.Errorf("invalid cursor: %w", err)
	}
	// JQL silently returns zero matches on bad date format. jqlCutoff
	// guarantees the format is valid; if it ever didn't, this query would
	// look exactly like "no activity" with no error.
	jql := fmt.Sprintf(`project = %s AND updated > %q ORDER BY updated ASC`,
		jqlEscape(project), cutoff)
	return searchIssues(ctx, c, jql, ver)
}

// fetchAndWrite pulls each issue's full body, splits it into issue + comment
// lines, and appends them to the per-issue file. Returns:
//
//   - written: count of issues whose issue line was successfully written
//     (NOT the count of issues attempted)
//   - maxUpdated: the highest fields.updated seen, including search-time
//     timestamps for issues that 404'd (those are permanently gone, so
//     advancing the cursor past them prevents an infinite re-discover loop)
//   - error: errors.Join of every transient failure encountered (network,
//     5xx, write errors). 404s are NOT included; they're logged and
//     accounted for as cursor-advance candidates.
//
// The loop continues past per-issue failures so a single 5xx on one issue
// doesn't block ingest of the others. The caller decides whether to
// advance the cursor based on whether any transient errors occurred.
func fetchAndWrite(
	ctx context.Context,
	c *jira.Client,
	s *store.FSStore,
	projDir paths.JiraProjectDir,
	ver APIVersion,
	issues []discoveredIssue,
) (int, string, error) {
	var errs []error
	var maxUpdated string
	written := 0

	for _, di := range issues {
		if ctx.Err() != nil {
			return written, maxUpdated, ctx.Err()
		}

		raw, err := getIssueRaw(ctx, c, di.Key, ver)
		if err != nil {
			if isNotFound(err) {
				// 404: issue moved or deleted. Permanent — log and let
				// the cursor advance past it via the search-time
				// updated, otherwise the next poll would re-discover
				// the same key, get the same 404, and never advance.
				slog.Warn("jira issue not found, skipping (cursor will advance past)",
					"key", di.Key, "search_updated", di.Updated)
				if di.Updated > maxUpdated {
					maxUpdated = di.Updated
				}
				continue
			}
			errs = append(errs, fmt.Errorf("fetch %s: %w", di.Key, err))
			continue
		}

		issueLine, commentLines, updated, err := splitIssueRaw(di.Key, []byte(raw))
		if err != nil {
			errs = append(errs, fmt.Errorf("parse %s: %w", di.Key, err))
			continue
		}

		file := projDir.IssueFile(di.Key)
		if err := s.AppendLine(file, issueLine); err != nil {
			errs = append(errs, fmt.Errorf("write issue %s: %w", di.Key, err))
			continue
		}
		written++
		for _, cl := range commentLines {
			if err := s.AppendLine(file, cl); err != nil {
				errs = append(errs, fmt.Errorf("write comment for %s: %w", di.Key, err))
			}
		}

		if updated > maxUpdated {
			maxUpdated = updated
		}
	}

	return written, maxUpdated, errors.Join(errs...)
}
