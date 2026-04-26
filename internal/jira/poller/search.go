package poller

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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

// searchKeys runs the given JQL against the configured API version and
// returns every issue key that matches, paginating as needed.
//
// On v3 (Cloud), pkg/jira.Search returns the first page plus a
// NextPageToken but the function does not accept the token back, so we
// drive subsequent pages by calling Client.Get directly with the token
// query parameter. On v2 (Server), SearchV2 accepts an offset, so we loop
// with from = 0, pageLimit, 2*pageLimit, ... until an empty page.
func searchKeys(ctx context.Context, c *jira.Client, jql string, ver APIVersion) ([]string, error) {
	if ver == APIVersionV2 {
		return searchKeysV2(c, jql)
	}
	return searchKeysV3(ctx, c, jql)
}

func searchKeysV2(c *jira.Client, jql string) ([]string, error) {
	var keys []string
	var from uint
	for {
		res, err := c.SearchV2(jql, from, pageLimit)
		if err != nil {
			return nil, fmt.Errorf("v2 search jql=%q from=%d: %w", jql, from, err)
		}
		for _, iss := range res.Issues {
			keys = append(keys, iss.Key)
		}
		if uint(len(res.Issues)) < pageLimit {
			break
		}
		from += pageLimit
	}
	return keys, nil
}

func searchKeysV3(ctx context.Context, c *jira.Client, jql string) ([]string, error) {
	res, err := c.Search(jql, pageLimit)
	if err != nil {
		return nil, fmt.Errorf("v3 search jql=%q: %w", jql, err)
	}
	keys := make([]string, 0, len(res.Issues))
	for _, iss := range res.Issues {
		keys = append(keys, iss.Key)
	}
	for !res.IsLast && res.NextPageToken != "" {
		res, err = nextPageV3(ctx, c, jql, res.NextPageToken)
		if err != nil {
			return nil, err
		}
		for _, iss := range res.Issues {
			keys = append(keys, iss.Key)
		}
	}
	return keys, nil
}

// nextPageV3 walks /search/jql past the first page using the token
// returned by pkg/jira.Search. The Search function itself does not accept
// nextPageToken, so this calls Client.Get directly (verified against live
// Jira Cloud, see protocol doc).
func nextPageV3(ctx context.Context, c *jira.Client, jql, token string) (*jira.SearchResult, error) {
	path := fmt.Sprintf("/search/jql?jql=%s&maxResults=%d&fields=*all&nextPageToken=%s",
		url.QueryEscape(jql), pageLimit, url.QueryEscape(token))
	resp, err := c.Get(ctx, path, nil)
	if err != nil {
		return nil, fmt.Errorf("v3 nextPage: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("v3 nextPage: HTTP %d: %s", resp.StatusCode, readBodyForError(resp.Body))
	}
	var out jira.SearchResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("v3 nextPage decode: %w", err)
	}
	return &out, nil
}

// getIssueRaw fetches a single issue's full HTTP body.
func getIssueRaw(c *jira.Client, key string, ver APIVersion) (string, error) {
	if ver == APIVersionV2 {
		return c.GetIssueV2Raw(key)
	}
	return c.GetIssueRaw(key)
}

// jqlEscape quotes a string value for inclusion inside a JQL expression.
// JQL string literals use double quotes with backslash-escaped backslashes
// and double quotes. Project keys never need this in practice (they're
// `[A-Z][A-Z0-9]*`), but JQL builders should never assume input shape.
func jqlEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(s) + `"`
}

// errorBodyLimit caps how much of an HTTP response body we embed in
// error messages. Jira's error bodies are typically a few hundred bytes
// of JSON; the cap prevents a hostile or accidentally-huge body from
// blowing up the log.
const errorBodyLimit = 4096

// readBodyForError reads up to errorBodyLimit bytes of body and returns
// them as a trimmed string suitable for embedding in an error message.
// Read failures are themselves swallowed because the caller already has
// a more relevant error (the non-200 status); we just want whatever
// body bytes we can get for debugging.
func readBodyForError(body io.Reader) string {
	b, _ := io.ReadAll(io.LimitReader(body, errorBodyLimit))
	return strings.TrimSpace(string(b))
}
