package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/anish749/pigeon/internal/api"
	daemonclient "github.com/anish749/pigeon/internal/daemon/client"
)

type ReactParams struct {
	Platform  string
	Account   string
	UserID    string // Slack DMs
	Channel   string // Slack channels/MPDMs
	Contact   string // WhatsApp
	MessageID string
	Emoji     string
	Remove    bool
}

func RunReact(p ReactParams) error {
	body, err := json.Marshal(api.ReactRequest{
		Platform:  p.Platform,
		Account:   p.Account,
		UserID:    p.UserID,
		Channel:   p.Channel,
		Contact:   p.Contact,
		MessageID: p.MessageID,
		Emoji:     p.Emoji,
		Remove:    p.Remove,
	})
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	client := daemonclient.DefaultPgnHTTPClient
	resp, err := client.Post("http://pigeon/api/react", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("daemon not reachable (is 'pigeon daemon start' running?): %w", err)
	}
	defer resp.Body.Close()

	var result api.ReactResponse
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

	action := "Reacted"
	if p.Remove {
		action = "Removed reaction"
	}
	target := p.Contact
	if target == "" {
		target = p.UserID
	}
	if target == "" {
		target = p.Channel
	}
	fmt.Printf("%s %s on message %s in %s\n", action, p.Emoji, p.MessageID, target)
	return nil
}
