package reader

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/anish749/pigeon/internal/paths"
)

// LinearIssue holds the parsed runtime fields and the raw serialized map.
type LinearIssue struct {
	ID         string         `json:"id"`
	Identifier string         `json:"identifier"`
	Title      string         `json:"title"`
	UpdatedAt  string         `json:"updatedAt"`
	State      *LinearState   `json:"state,omitempty"`
	Assignee   *LinearPerson  `json:"assignee,omitempty"`
	Raw        map[string]any `json:"-"` // full line for display
}

// LinearState holds issue state info.
type LinearState struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// LinearPerson holds basic person info.
type LinearPerson struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
}

// LinearComment holds a parsed Linear comment.
type LinearComment struct {
	ID        string         `json:"id"`
	Body      string         `json:"body"`
	CreatedAt string         `json:"createdAt"`
	User      *LinearPerson  `json:"user,omitempty"`
	ParentID  string         `json:"-"` // extracted from parent.id
	Raw       map[string]any `json:"-"`
}

// LinearIssueResult holds the output of reading a single Linear issue.
type LinearIssueResult struct {
	Issue    LinearIssue
	Comments []LinearComment
}

// LinearListResult holds the output of listing Linear issues.
type LinearListResult struct {
	Issues []LinearIssue
}

// ReadLinearIssue reads a single Linear issue file, deduplicates, and
// returns the latest issue snapshot with all unique comments.
func ReadLinearIssue(accountDir paths.AccountDir, identifier string) (*LinearIssueResult, error) {
	issuesDir := filepath.Join(accountDir.Path(), "issues")
	issuePath := filepath.Join(issuesDir, identifier+paths.FileExt)

	data, err := os.ReadFile(issuePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no issue %s found", identifier)
		}
		return nil, fmt.Errorf("read %s: %w", issuePath, err)
	}

	var issues []LinearIssue
	var comments []LinearComment
	var errs []error
	for _, rawLine := range splitLines(data) {
		var raw map[string]any
		if err := json.Unmarshal([]byte(rawLine), &raw); err != nil {
			errs = append(errs, fmt.Errorf("parse line in %s: %w", identifier, err))
			continue
		}
		lineType, _ := raw["type"].(string)
		switch lineType {
		case "issue":
			var issue LinearIssue
			if err := json.Unmarshal([]byte(rawLine), &issue); err != nil {
				errs = append(errs, fmt.Errorf("parse issue line in %s: %w", identifier, err))
				continue
			}
			issue.Raw = raw
			issues = append(issues, issue)
		case "comment":
			var comment LinearComment
			if err := json.Unmarshal([]byte(rawLine), &comment); err != nil {
				errs = append(errs, fmt.Errorf("parse comment line in %s: %w", identifier, err))
				continue
			}
			comment.Raw = raw
			// Extract parent ID from nested object.
			if parent, ok := raw["parent"].(map[string]any); ok {
				if pid, ok := parent["id"].(string); ok {
					comment.ParentID = pid
				}
			}
			comments = append(comments, comment)
		}
	}

	// Dedup issues by ID (keep last).
	var latestIssue LinearIssue
	if len(issues) > 0 {
		issueByID := make(map[string]LinearIssue)
		for _, iss := range issues {
			issueByID[iss.ID] = iss
		}
		// There should be exactly one unique issue ID per file.
		for _, iss := range issueByID {
			latestIssue = iss
		}
	}

	// Dedup comments by ID (keep last).
	comments = dedupLinearComments(comments)

	// Sort comments by creation time.
	sort.SliceStable(comments, func(i, j int) bool {
		return comments[i].CreatedAt < comments[j].CreatedAt
	})

	return &LinearIssueResult{
		Issue:    latestIssue,
		Comments: comments,
	}, errors.Join(errs...)
}

// ListLinearIssues lists recently updated issues across all issue files.
func ListLinearIssues(accountDir paths.AccountDir, filters Filters) (*LinearListResult, error) {
	issuesDir := filepath.Join(accountDir.Path(), "issues")
	entries, err := os.ReadDir(issuesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return &LinearListResult{}, nil
		}
		return nil, fmt.Errorf("list linear issues: %w", err)
	}

	var issues []LinearIssue
	var errs []error
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != paths.FileExt {
			continue
		}
		data, err := os.ReadFile(filepath.Join(issuesDir, e.Name()))
		if err != nil {
			errs = append(errs, fmt.Errorf("read %s: %w", e.Name(), err))
			continue
		}
		// Read the last issue line in the file.
		issue := lastIssueLine(data)
		if issue == nil {
			continue
		}
		issues = append(issues, *issue)
	}

	// Filter by time window.
	if filters.Since > 0 || filters.Date != "" {
		issues = filterLinearIssues(issues, filters)
	} else {
		// Default: last 7 days.
		issues = filterLinearIssues(issues, Filters{Since: 7 * 24 * time.Hour})
	}

	// Sort by updatedAt descending.
	sort.SliceStable(issues, func(i, j int) bool {
		return issues[i].UpdatedAt > issues[j].UpdatedAt
	})

	return &LinearListResult{Issues: issues}, errors.Join(errs...)
}

// FindLinearIssue fuzzy-matches an identifier or title against issue files.
func FindLinearIssue(accountDir paths.AccountDir, selector string) (string, error) {
	issuesDir := filepath.Join(accountDir.Path(), "issues")
	entries, err := os.ReadDir(issuesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("no linear issue data")
		}
		return "", fmt.Errorf("read linear issues dir: %w", err)
	}

	q := strings.ToLower(selector)
	var matches []string

	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != paths.FileExt {
			continue
		}
		identifier := strings.TrimSuffix(e.Name(), paths.FileExt)
		// Exact identifier match.
		if strings.EqualFold(identifier, selector) {
			return identifier, nil
		}
		// Fuzzy match on identifier.
		if strings.Contains(strings.ToLower(identifier), q) {
			matches = append(matches, identifier)
			continue
		}
		// Also try title match.
		data, err := os.ReadFile(filepath.Join(issuesDir, e.Name()))
		if err != nil {
			return "", fmt.Errorf("read %s for title match: %w", e.Name(), err)
		}
		issue := lastIssueLine(data)
		if issue != nil && strings.Contains(strings.ToLower(issue.Title), q) {
			matches = append(matches, identifier)
		}
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no linear issue matching %q", selector)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("ambiguous linear issue %q — matches: %s", selector, strings.Join(matches, ", "))
	}
}

// lastIssueLine reads the last issue-type line from a file.
func lastIssueLine(data []byte) *LinearIssue {
	var last *LinearIssue
	for _, rawLine := range splitLines(data) {
		var raw map[string]any
		if err := json.Unmarshal([]byte(rawLine), &raw); err != nil {
			continue
		}
		if lineType, _ := raw["type"].(string); lineType != "issue" {
			continue
		}
		var issue LinearIssue
		if err := json.Unmarshal([]byte(rawLine), &issue); err != nil {
			continue
		}
		issue.Raw = raw
		last = &issue
	}
	return last
}

func dedupLinearComments(comments []LinearComment) []LinearComment {
	lastIndex := make(map[string]int, len(comments))
	for i, c := range comments {
		lastIndex[c.ID] = i
	}
	var result []LinearComment
	for i, c := range comments {
		if lastIndex[c.ID] == i {
			result = append(result, c)
		}
	}
	return result
}

func filterLinearIssues(issues []LinearIssue, filters Filters) []LinearIssue {
	var result []LinearIssue
	for _, issue := range issues {
		t, err := time.Parse(time.RFC3339, issue.UpdatedAt)
		if err != nil {
			continue
		}
		if filters.Since > 0 && time.Since(t) > filters.Since {
			continue
		}
		if filters.Date != "" && t.Format("2006-01-02") != filters.Date {
			continue
		}
		result = append(result, issue)
	}
	return result
}
