package poller

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"

	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
	"github.com/anish749/pigeon/internal/store/modelv1"
)

// PollIssues runs one sync cycle for Linear issues. Returns the number of
// issues processed and any error.
func PollIssues(s *store.FSStore, account paths.AccountDir, workspace string, cursors *store.LinearCursors) (int, error) {
	cursor := cursors.Issues.UpdatedAfter

	issues, err := queryIssues(workspace, cursor)
	if err != nil {
		return 0, fmt.Errorf("query issues: %w", err)
	}
	if len(issues) == 0 {
		return 0, nil
	}

	var errs []error
	maxUpdatedAt := cursor
	linearDir := account.Linear()

	for _, issue := range issues {
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
		comments, err := fetchComments(workspace, identifier)
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

	cursors.Issues.UpdatedAfter = maxUpdatedAt
	return len(issues), errors.Join(errs...)
}

// queryIssues runs `linear issue query` and returns the list of issue objects.
func queryIssues(workspace, cursor string) ([]map[string]any, error) {
	args := []string{"issue", "query", "-j", "--all-teams", "--all-states", "--limit=0", "--no-pager"}
	if workspace != "" {
		args = append(args, "--workspace", workspace)
	}
	if cursor != "" {
		args = append(args, "--updated-after="+cursor)
	}

	out, err := runLinear(args...)
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
func fetchComments(workspace, identifier string) ([]map[string]any, error) {
	args := []string{"issue", "view", identifier, "-j", "--no-download", "--no-pager"}
	if workspace != "" {
		args = append(args, "--workspace", workspace)
	}

	out, err := runLinear(args...)
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
		Type: modelv1.LineIssue,
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
func runLinear(args ...string) ([]byte, error) {
	cmd := exec.Command("linear", args...)
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
