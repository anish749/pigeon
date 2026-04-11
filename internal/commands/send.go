package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/api"
	daemonclient "github.com/anish749/pigeon/internal/daemon/client"
	"github.com/anish749/pigeon/internal/paths"
)

// slackTarget returns a *SlackTarget if either field is set, or nil.
func slackTarget(userID, channel string) *api.SlackTarget {
	if userID == "" && channel == "" {
		return nil
	}
	return &api.SlackTarget{UserID: userID, Channel: channel}
}

type SendParams struct {
	Platform  string
	Account   string
	UserID    string // Slack DMs
	Channel   string // Slack channels/MPDMs
	Contact   string // WhatsApp
	Message   string
	Thread    string
	Broadcast bool
	PostAt    string // Unix timestamp — schedule for later (Slack only)
	AsUser    bool
	DryRun    bool
	Force     bool
}

func RunSend(p SendParams) error {
	// Validate thread parent exists locally before sending.
	if p.Thread != "" && !p.Force {
		accountDir := paths.DefaultDataRoot().AccountFor(account.New(p.Platform, p.Account)).Path()
		found, err := messageExists(accountDir, p.Thread)
		if err != nil {
			return fmt.Errorf("validate thread %s: %w", p.Thread, err)
		}
		if !found {
			return fmt.Errorf(
				"thread %s not found — check the timestamp with "+
					"'pigeon search' or 'pigeon read'. "+
					"Use --force to send anyway (Slack will post as a top-level message if the thread doesn't exist).",
				p.Thread)
		}
	}

	req := api.SendRequest{
		Platform:  p.Platform,
		Account:   p.Account,
		Slack:     slackTarget(p.UserID, p.Channel),
		Contact:   p.Contact,
		Message:   p.Message,
		Thread:    p.Thread,
		Broadcast: p.Broadcast,
		PostAt:    p.PostAt,
		AsUser:    p.AsUser,
		DryRun:    p.DryRun,
		Force:     p.Force,
		SessionID: os.Getenv("PIGEON_SESSION_ID"),
	}
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	client := daemonclient.DefaultPgnHTTPClient
	resp, err := client.Post("http://pigeon/api/send", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("daemon not reachable (is 'pigeon daemon start' running?): %w", err)
	}
	defer resp.Body.Close()

	var result api.SendResponse
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

	if result.OutboxID != "" {
		fmt.Printf("Submitted for review (ID: %s)\n", result.OutboxID)
		return nil
	}

	if p.DryRun {
		fmt.Printf("Dry run — would send to %s (%s) as %s\n", result.ChannelName, result.ChannelID, result.SendAs)
	} else if result.ScheduledMessageID != "" {
		fmt.Printf("Scheduled to %s (ID: %s)\n", req.Target(), result.ScheduledMessageID)
	} else {
		fmt.Printf("Sent to %s at %s\n", req.Target(), result.Timestamp)
	}
	return nil
}

