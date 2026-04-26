package poller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	jira "github.com/ankitpokhrel/jira-cli/pkg/jira"
)

// APIVersion selects v3 (Cloud) vs v2 (Server / Data Center) endpoint
// dispatch. The two versions differ in pagination, ADF vs wiki-markup body
// representation, and the available pkg/jira methods.
type APIVersion int

const (
	APIVersionV3 APIVersion = iota
	APIVersionV2
)

// pageLimit is the per-call result cap. Jira Cloud's /search/jql enforces a
// hard server-side range of [1, 5000] on the maxResults parameter; 100 is
// well within that and matches what jira-cli's own backfill uses.
const pageLimit = 100

// httpError represents a non-2xx response from a direct Client.Get/GetV2
// call. Carries the status code so callers can branch on specific values
// (e.g. 404 for "issue gone, advance the cursor past it") via errors.As.
// The Body is the response body verbatim — useful for debugging and the
// authoritative description of why Jira rejected the request.
type httpError struct {
	StatusCode int
	Status     string
	Body       string
	Path       string
}

func (e *httpError) Error() string {
	return fmt.Sprintf("HTTP %s on %s: %s", e.Status, e.Path, e.Body)
}

// isNotFound reports whether err wraps a 404 from the Jira REST API.
// Used to decide whether to advance the cursor past a missing issue
// (yes — 404 is permanent) or to retry next poll (no — transient
// errors stay).
func isNotFound(err error) bool {
	var he *httpError
	if errors.As(err, &he) {
		return he.StatusCode == http.StatusNotFound
	}
	return false
}

// discoveredIssue is what searchKeys returns: the key plus the search-time
// `updated` timestamp from fields.updated. The poller uses Updated as a
// cursor-advance fallback when a per-issue fetch fails permanently (404).
type discoveredIssue struct {
	Key     string
	Updated string
}

// searchIssues runs the given JQL against the configured API version and
// returns every matching issue's key + updated timestamp, paginating as
// needed. Honors ctx end-to-end — pkg/jira's convenience methods take
// no context, so we use Client.Get/GetV2 directly.
func searchIssues(ctx context.Context, c *jira.Client, jql string, ver APIVersion) ([]discoveredIssue, error) {
	if ver == APIVersionV2 {
		return searchIssuesV2(ctx, c, jql)
	}
	return searchIssuesV3(ctx, c, jql)
}

// searchIssuesV3 paginates Jira Cloud's /search/jql endpoint via
// nextPageToken until IsLast is true. We construct the path by hand
// rather than calling pkg/jira.Search because that function uses
// context.Background() and does not accept the next-page token; both
// matter for a daemon doing graceful shutdown over many pages.
func searchIssuesV3(ctx context.Context, c *jira.Client, jql string) ([]discoveredIssue, error) {
	var out []discoveredIssue
	var token string
	for {
		path := buildSearchPathV3(jql, pageLimit, token)
		var page jira.SearchResult
		if err := getJSON(ctx, c, path, false, &page); err != nil {
			return nil, fmt.Errorf("v3 search jql=%q token=%q: %w", jql, token, err)
		}
		for _, iss := range page.Issues {
			out = append(out, discoveredIssue{Key: iss.Key, Updated: iss.Fields.Updated})
		}
		if page.IsLast || page.NextPageToken == "" {
			return out, nil
		}
		token = page.NextPageToken
	}
}

// searchIssuesV2 paginates Jira Server's /search endpoint via the
// startAt offset. Server has no nextPageToken; pages end when the
// returned slice is shorter than the limit.
func searchIssuesV2(ctx context.Context, c *jira.Client, jql string) ([]discoveredIssue, error) {
	var out []discoveredIssue
	var from uint
	for {
		path := buildSearchPathV2(jql, from, pageLimit)
		var page jira.SearchResult
		if err := getJSON(ctx, c, path, true, &page); err != nil {
			return nil, fmt.Errorf("v2 search jql=%q from=%d: %w", jql, from, err)
		}
		for _, iss := range page.Issues {
			out = append(out, discoveredIssue{Key: iss.Key, Updated: iss.Fields.Updated})
		}
		if uint(len(page.Issues)) < pageLimit {
			return out, nil
		}
		from += pageLimit
	}
}

func buildSearchPathV3(jql string, limit uint, nextPageToken string) string {
	q := fmt.Sprintf("/search/jql?jql=%s&maxResults=%d&fields=*all", url.QueryEscape(jql), limit)
	if nextPageToken != "" {
		q += "&nextPageToken=" + url.QueryEscape(nextPageToken)
	}
	return q
}

func buildSearchPathV2(jql string, from, limit uint) string {
	return fmt.Sprintf("/search?jql=%s&startAt=%d&maxResults=%d", url.QueryEscape(jql), from, limit)
}

// getIssueRaw fetches a single issue's full HTTP body via Client.Get
// (or GetV2 for Server). Returns an *httpError on non-200, which the
// caller inspects for 404 to decide cursor behavior.
func getIssueRaw(ctx context.Context, c *jira.Client, key string, ver APIVersion) (string, error) {
	path := "/issue/" + key
	resp, err := getRaw(ctx, c, path, ver == APIVersionV2)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", &httpError{
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
			Body:       readBody(resp.Body),
			Path:       path,
		}
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read issue body: %w", err)
	}
	return string(body), nil
}

// getJSON issues a Get/GetV2 request, errors on non-200, and decodes the
// response body into out.
func getJSON(ctx context.Context, c *jira.Client, path string, v2 bool, out any) error {
	resp, err := getRaw(ctx, c, path, v2)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &httpError{
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
			Body:       readBody(resp.Body),
			Path:       path,
		}
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func getRaw(ctx context.Context, c *jira.Client, path string, v2 bool) (*http.Response, error) {
	if v2 {
		return c.GetV2(ctx, path, nil)
	}
	return c.Get(ctx, path, nil)
}

// readBody returns the entire response body. No size cap — error messages
// should include the full server response so debugging never has to ask
// "what was actually wrong?"
func readBody(body io.Reader) string {
	b, _ := io.ReadAll(body)
	return strings.TrimSpace(string(b))
}

// jqlEscape quotes a string value for inclusion inside a JQL expression.
// JQL string literals use double quotes with backslash-escaped backslashes
// and double quotes. Project keys never need this in practice (they're
// `[A-Z][A-Z0-9]*`), but JQL builders should never assume input shape.
func jqlEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(s) + `"`
}
