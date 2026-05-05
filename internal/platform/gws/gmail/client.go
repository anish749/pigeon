// Package gmail fetches Gmail messages via the gws CLI.
package gmail

import (
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/anish749/pigeon/internal/platform/gws"
	"github.com/anish749/pigeon/internal/store/modelv1"
)

// Client wraps a gws.Client for Gmail API calls.
type Client struct {
	gws *gws.Client
}

// NewClient creates a Gmail client backed by the given gws.Client.
func NewClient(g *gws.Client) *Client {
	return &Client{gws: g}
}

// gmailProfile is the subset of users.getProfile we need.
type gmailProfile struct {
	HistoryID string `json:"historyId"`
}

// gmailHistoryResponse from users.history.list.
type gmailHistoryResponse struct {
	History       []gmailHistoryRecord `json:"history"`
	HistoryID     string               `json:"historyId"`
	NextPageToken string               `json:"nextPageToken"`
}

type gmailHistoryRecord struct {
	MessagesAdded   []gmailHistoryMsg `json:"messagesAdded"`
	MessagesDeleted []gmailHistoryMsg `json:"messagesDeleted"`
}

type gmailHistoryMsg struct {
	Message gmailMessageRef `json:"message"`
}

type gmailMessageRef struct {
	ID       string   `json:"id"`
	ThreadID string   `json:"threadId"`
	LabelIDs []string `json:"labelIds"`
}

// gmailRawMessage is the response from users.messages.get with format=raw.
type gmailRawMessage struct {
	ID           string   `json:"id"`
	ThreadID     string   `json:"threadId"`
	InternalDate string   `json:"internalDate"` // unix millis as string
	LabelIDs     []string `json:"labelIds"`
	Snippet      string   `json:"snippet"`
	Raw          string   `json:"raw"` // base64url-encoded RFC 2822
}

// GetHistoryID fetches the current historyId from the user's profile.
func (c *Client) GetHistoryID() (string, error) {
	params := gws.ParamsJSON(map[string]string{"userId": "me"})
	var profile gmailProfile
	if err := c.gws.RunParsed(&profile, "gmail", "users", "getProfile", "--params", params); err != nil {
		return "", fmt.Errorf("get gmail profile: %w", err)
	}
	if profile.HistoryID == "" {
		return "", fmt.Errorf("get gmail profile: empty historyId")
	}
	return profile.HistoryID, nil
}

// ListHistory fetches message changes since startHistoryId.
// Paginates through all pages. Returns added message IDs, deleted message IDs,
// and the new historyId.
func (c *Client) ListHistory(startHistoryId string) (added []string, deleted []string, newHistoryId string, err error) {
	addedSet := make(map[string]bool)
	deletedSet := make(map[string]bool)

	pageToken := ""
	for {
		params := map[string]string{
			"userId":         "me",
			"startHistoryId": startHistoryId,
		}
		if pageToken != "" {
			params["pageToken"] = pageToken
		}

		var resp gmailHistoryResponse
		if err := c.gws.RunParsed(&resp, "gmail", "users", "history", "list", "--params", gws.ParamsJSON(params)); err != nil {
			return nil, nil, "", err
		}

		for _, rec := range resp.History {
			for _, m := range rec.MessagesAdded {
				addedSet[m.Message.ID] = true
			}
			for _, m := range rec.MessagesDeleted {
				deletedSet[m.Message.ID] = true
			}
		}

		newHistoryId = resp.HistoryID

		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}

	// A message that was added and then deleted in the same poll window
	// should only appear as deleted.
	for id := range deletedSet {
		delete(addedSet, id)
	}

	for id := range addedSet {
		added = append(added, id)
	}
	for id := range deletedSet {
		deleted = append(deleted, id)
	}
	return added, deleted, newHistoryId, nil
}

// messagesListResponse from users.messages.list.
type messagesListResponse struct {
	Messages      []gmailMessageRef `json:"messages"`
	NextPageToken string            `json:"nextPageToken"`
}

// ListMessages enumerates message IDs matching a Gmail search query.
// Paginates through all pages. Returns message IDs only (no content).
func (c *Client) ListMessages(query string) ([]string, error) {
	params := map[string]string{
		"userId":     "me",
		"q":          query,
		"maxResults": "500",
	}

	var ids []string
	for {
		var resp messagesListResponse
		if err := c.gws.RunParsed(&resp, "gmail", "users", "messages", "list", "--params", gws.ParamsJSON(params)); err != nil {
			return nil, fmt.Errorf("list gmail messages: %w", err)
		}

		for _, m := range resp.Messages {
			ids = append(ids, m.ID)
		}

		if resp.NextPageToken != "" {
			params["pageToken"] = resp.NextPageToken
			continue
		}
		break
	}

	return ids, nil
}

// GetMessage fetches a raw message by ID and parses it with enmime.
func (c *Client) GetMessage(messageID string) (*modelv1.EmailLine, error) {
	params := gws.ParamsJSON(map[string]string{
		"userId": "me",
		"id":     messageID,
		"format": "raw",
	})

	var msg gmailRawMessage
	if err := c.gws.RunParsed(&msg, "gmail", "users", "messages", "get", "--params", params); err != nil {
		return nil, fmt.Errorf("get gmail message %s: %w", messageID, err)
	}

	ts, err := parseInternalDate(msg.InternalDate)
	if err != nil {
		return nil, fmt.Errorf("parse date for message %s: %w", messageID, err)
	}

	parsed, err := parseRawMessage(msg.Raw)
	if err != nil {
		return nil, fmt.Errorf("parse message %s: %w", messageID, err)
	}
	for _, w := range parsed.warnings {
		slog.Warn("recovered malformed mime part",
			"messageId", messageID, "subject", parsed.subject, "warning", w)
	}

	return &modelv1.EmailLine{
		ID:       msg.ID,
		ThreadID: msg.ThreadID,
		Ts:       ts,
		From:     parsed.fromAddr,
		FromName: parsed.fromName,
		To:       parsed.to,
		CC:       parsed.cc,
		Subject:  parsed.subject,
		Labels:   msg.LabelIDs,
		Snippet:  msg.Snippet,
		Text:     parsed.text,
		HTML:     parsed.html,
		Attach:   parsed.attachments,
	}, nil
}

// parseInternalDate converts Gmail's internalDate (unix millis string) to time.Time.
func parseInternalDate(s string) (time.Time, error) {
	millis, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse internalDate %q: %w", s, err)
	}
	return time.UnixMilli(millis), nil
}
