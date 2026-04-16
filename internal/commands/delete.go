package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/api"
	daemonclient "github.com/anish749/pigeon/internal/daemon/client"
	"github.com/anish749/pigeon/internal/paths"
)

type DeleteParams struct {
	Platform  string
	Account   string
	UserID    string // Slack DMs
	Channel   string // Slack channels/MPDMs
	MessageID string
}

func RunDelete(p DeleteParams) error {
	// Validate the target message exists locally before deleting.
	accountDir := paths.DefaultDataRoot().AccountFor(account.New(p.Platform, p.Account)).Path()
	found, err := messageExists(accountDir, p.MessageID)
	if err != nil {
		return fmt.Errorf("validate message %s: %w", p.MessageID, err)
	}
	if !found {
		return fmt.Errorf(
			"message %s not found — check the ID with 'pigeon search' or 'pigeon read'.",
			p.MessageID)
	}

	body, err := json.Marshal(api.DeleteRequest{
		Platform:  p.Platform,
		Account:   p.Account,
		Slack:     slackTarget(p.UserID, p.Channel),
		MessageID: p.MessageID,
	})
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	client := daemonclient.DefaultPgnHTTPClient
	resp, err := client.Post("http://pigeon/api/delete", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("daemon not reachable (is 'pigeon daemon start' running?): %w", err)
	}
	defer resp.Body.Close()

	var result api.DeleteResponse
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return fmt.Errorf("unexpected response: %s", string(data))
	}

	if !result.OK {
		return fmt.Errorf("%s", result.Error)
	}

	target := p.UserID
	if target == "" {
		target = p.Channel
	}
	fmt.Printf("Deleted message %s in %s\n", p.MessageID, target)
	return nil
}
