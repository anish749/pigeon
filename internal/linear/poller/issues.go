package poller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"time"

	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
	"github.com/anish749/pigeon/internal/store/modelv1"
)

// backfillDays is the number of days to look back for recently closed issues
// during first-run backfill.
const backfillDays = 90

// PollIssues runs one sync cycle for Linear issues. On first run (no cursor),
// it backfills active issues + recently closed issues. On subsequent runs, it
// fetches only issues updated since the cursor. Returns the number of issues
// processed and any error.
func PollIssues(ctx context.Context, s *store.FSStore, account paths.AccountDir, workspace string, cursors *store.LinearCursors) (int, error) {
	cursor := cursors.Issues.UpdatedAfter

	var issues []map[string]any
	var err error

	if cursor == "" {
		issues, err = backfillIssues(ctx, workspace)
	} else {
		issues, err = queryIssues(ctx, workspace, "--all-states", "--updated-after="+cursor)
	}
	if err != nil {
		return 0, fmt.Errorf("query issues: %w", err)
	}
	if len(issues) == 0 {
		return 0, nil
	}

	n, maxUpdatedAt, err := writeIssues(ctx, s, account.Linear(), workspace, issues)

	// Only advance the cursor if every issue was processed without error.
	// If any issue failed (write, comment fetch, etc.), the cursor stays
	// put so the next poll retries the entire batch.
	if err == nil && maxUpdatedAt > cursor {
		cursors.Issues.UpdatedAfter = maxUpdatedAt
	}
	return n, err
}

// backfillIssues fetches two sets for first-run backfill:
// 1. All active issues (triage, backlog, unstarted, started)
// 2. Recently closed issues (completed, canceled) from the last 90 days
func backfillIssues(ctx context.Context, workspace string) ([]map[string]any, error) {
	slog.Info("linear backfill starting", "workspace", workspace)

	active, err := queryIssues(ctx, workspace,
		"-s", "triage", "-s", "backlog", "-s", "unstarted", "-s", "started")
	if err != nil {
		return nil, fmt.Errorf("query active issues: %w", err)
	}

	cutoff := time.Now().UTC().AddDate(0, 0, -backfillDays).Format(time.RFC3339)
	closed, err := queryIssues(ctx, workspace,
		"-s", "completed", "-s", "canceled", "--updated-after="+cutoff)
	if err != nil {
		return nil, fmt.Errorf("query closed issues: %w", err)
	}

	slog.Info("linear backfill fetched", "workspace", workspace,
		"active", len(active), "closed", len(closed))

	return append(active, closed...), nil
}

// writeIssues writes a batch of issues and their comments to disk. Returns
// the count of issues processed, the maximum updatedAt seen, and any error.
func writeIssues(ctx context.Context, s *store.FSStore, linearDir paths.LinearDir, workspace string, issues []map[string]any) (int, string, error) {
	var errs []error
	var maxUpdatedAt string

	for _, issue := range issues {
		if ctx.Err() != nil {
			return 0, "", ctx.Err()
		}

		identifier, _ := issue["identifier"].(string)
		updatedAt, _ := issue["updatedAt"].(string)
		if identifier == "" {
			errs = append(errs, fmt.Errorf("issue missing identifier"))
			continue
		}

		// Write the issue snapshot.
		issueLine, err := issueToLine(issue)
		if err != nil {
			errs = append(errs, fmt.Errorf("marshal issue %s: %w", identifier, err))
			continue
		}
		if err := s.AppendLine(linearDir.IssueFile(identifier), issueLine); err != nil {
			errs = append(errs, fmt.Errorf("write issue %s: %w", identifier, err))
			continue
		}

		// Fetch the full issue view (includes comments).
		comments, err := fetchComments(ctx, workspace, identifier)
		if err != nil {
			errs = append(errs, fmt.Errorf("fetch comments for %s: %w", identifier, err))
		}
		for _, comment := range comments {
			commentLine, err := commentToLine(comment)
			if err != nil {
				errs = append(errs, fmt.Errorf("marshal comment for %s: %w", identifier, err))
				continue
			}
			if err := s.AppendLine(linearDir.IssueFile(identifier), commentLine); err != nil {
				errs = append(errs, fmt.Errorf("write comment for %s: %w", identifier, err))
			}
		}

		if updatedAt > maxUpdatedAt {
			maxUpdatedAt = updatedAt
		}
	}

	return len(issues), maxUpdatedAt, errors.Join(errs...)
}

// queryIssues runs `linear issue query` with the given extra args and returns
// the list of issue objects.
func queryIssues(ctx context.Context, workspace string, extraArgs ...string) ([]map[string]any, error) {
	args := []string{"issue", "query", "-j", "--all-teams", "--limit=0", "--no-pager",
		"--workspace", workspace}
	args = append(args, extraArgs...)

	out, err := runLinear(ctx, args...)
	if err != nil {
		return nil, err
	}

	var result struct {
		Nodes []map[string]any `json:"nodes"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("parse query output: %w", err)
	}
	return result.Nodes, nil
}

// fetchComments runs `linear issue view` and extracts comments from the response.
func fetchComments(ctx context.Context, workspace, identifier string) ([]map[string]any, error) {
	args := []string{"issue", "view", identifier, "-j", "--no-download", "--no-pager",
		"--workspace", workspace}

	out, err := runLinear(ctx, args...)
	if err != nil {
		return nil, err
	}

	var result struct {
		Comments struct {
			Nodes []map[string]any `json:"nodes"`
		} `json:"comments"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("parse view output: %w", err)
	}
	return result.Comments.Nodes, nil
}

// issueToLine wraps a raw issue map into a Line ready for storage.
func issueToLine(issue map[string]any) (modelv1.Line, error) {
	raw, err := json.Marshal(issue)
	if err != nil {
		return modelv1.Line{}, fmt.Errorf("re-marshal issue: %w", err)
	}
	var runtime modelv1.LinearIssueRuntime
	if err := json.Unmarshal(raw, &runtime); err != nil {
		return modelv1.Line{}, fmt.Errorf("parse issue runtime: %w", err)
	}
	return modelv1.Line{
		Type: modelv1.LineLinearIssue,
		Issue: &modelv1.LinearIssue{
			Runtime:    runtime,
			Serialized: issue,
		},
	}, nil
}

// commentToLine wraps a raw comment map into a Line ready for storage.
func commentToLine(comment map[string]any) (modelv1.Line, error) {
	raw, err := json.Marshal(comment)
	if err != nil {
		return modelv1.Line{}, fmt.Errorf("re-marshal comment: %w", err)
	}
	var runtime modelv1.LinearCommentRuntime
	if err := json.Unmarshal(raw, &runtime); err != nil {
		return modelv1.Line{}, fmt.Errorf("parse comment runtime: %w", err)
	}
	return modelv1.Line{
		Type: modelv1.LineLinearComment,
		LinearComment: &modelv1.LinearComment{
			Runtime:    runtime,
			Serialized: comment,
		},
	}, nil
}

// runLinear executes the linear CLI with the given arguments and returns stdout.
func runLinear(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "linear", args...)
	cmd.Env = append(cmd.Environ(), "PAGER=cat")
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			slog.Error("linear cli failed", "args", args, "stderr", string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("run linear %v: %w", args, err)
	}
	return out, nil
}
