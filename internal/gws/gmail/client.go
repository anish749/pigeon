// Package gmail fetches Gmail messages via the gws CLI.
package gmail

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/anish749/pigeon/internal/gws"
	"github.com/anish749/pigeon/internal/gws/model"
)

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

// gmailMessage is the full message response from users.messages.get.
type gmailMessage struct {
	ID           string       `json:"id"`
	ThreadID     string       `json:"threadId"`
	InternalDate string       `json:"internalDate"` // unix millis as string
	LabelIDs     []string     `json:"labelIds"`
	Snippet      string       `json:"snippet"`
	Payload      gmailPayload `json:"payload"`
}

// GetHistoryID fetches the current historyId from the user's profile.
func GetHistoryID() (string, error) {
	params := gws.ParamsJSON(map[string]string{"userId": "me"})
	var profile gmailProfile
	if err := gws.RunParsed(&profile, "gmail", "users", "getProfile", "--params", params); err != nil {
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
func ListHistory(startHistoryId string) (added []string, deleted []string, newHistoryId string, err error) {
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
		if err := gws.RunParsed(&resp, "gmail", "users", "history", "list", "--params", gws.ParamsJSON(params)); err != nil {
			return nil, nil, "", fmt.Errorf("list gmail history: %w", err)
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

// GetMessage fetches a full message by ID and converts it to an EmailLine.
func GetMessage(messageID string) (*model.EmailLine, error) {
	params := gws.ParamsJSON(map[string]string{
		"userId": "me",
		"id":     messageID,
		"format": "full",
	})

	var msg gmailMessage
	if err := gws.RunParsed(&msg, "gmail", "users", "messages", "get", "--params", params); err != nil {
		return nil, fmt.Errorf("get gmail message %s: %w", messageID, err)
	}

	ts, err := parseInternalDate(msg.InternalDate)
	if err != nil {
		return nil, fmt.Errorf("parse date for message %s: %w", messageID, err)
	}

	headers := headerMap(msg.Payload.Headers)
	fromName, fromEmail := parseFrom(headers["from"])
	to := parseAddressList(headers["to"])
	cc := parseAddressList(headers["cc"])

	body := ExtractBody(msg.Payload)
	attachments := ExtractAttachments(msg.Payload)

	return &model.EmailLine{
		Type:     "email",
		ID:       msg.ID,
		ThreadID: msg.ThreadID,
		Ts:       ts,
		From:     fromEmail,
		FromName: fromName,
		To:       to,
		CC:       cc,
		Subject:  headers["subject"],
		Labels:   msg.LabelIDs,
		Snippet:  msg.Snippet,
		Text:     body,
		Attach:   attachments,
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

// headerMap builds a case-insensitive lookup from Gmail headers.
// Duplicate headers: first occurrence wins.
func headerMap(headers []gmailHeader) map[string]string {
	m := make(map[string]string, len(headers))
	for _, h := range headers {
		key := strings.ToLower(h.Name)
		if _, exists := m[key]; !exists {
			m[key] = h.Value
		}
	}
	return m
}

// parseFrom extracts display name and email from a From header value.
// Handles "Display Name <email@example.com>" and bare "email@example.com".
func parseFrom(from string) (name, email string) {
	from = strings.TrimSpace(from)
	if from == "" {
		return "", ""
	}

	if idx := strings.LastIndex(from, "<"); idx >= 0 {
		name = strings.TrimSpace(from[:idx])
		// Strip surrounding quotes from display name.
		name = strings.Trim(name, "\"")
		end := strings.Index(from[idx:], ">")
		if end >= 0 {
			email = from[idx+1 : idx+end]
		} else {
			email = from[idx+1:]
		}
		return name, email
	}

	// Bare email address.
	return "", from
}

// parseAddressList extracts email addresses from a comma-separated header value.
// Handles "Name <email>" and bare "email" formats.
func parseAddressList(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	var emails []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if idx := strings.LastIndex(part, "<"); idx >= 0 {
			end := strings.Index(part[idx:], ">")
			if end >= 0 {
				emails = append(emails, part[idx+1:idx+end])
			}
		} else {
			emails = append(emails, part)
		}
	}
	return emails
}
