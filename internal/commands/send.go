package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"

	"github.com/anish/claude-msg-utils/internal/api"
	"github.com/anish/claude-msg-utils/internal/paths"
)

type SendParams struct {
	Platform  string
	Account   string
	Contact   string
	Message   string
	Thread    string
	Broadcast bool
	AsUser    bool
	DryRun    bool
}

func RunSend(p SendParams) error {
	body, err := json.Marshal(api.SendRequest{
		Platform:  p.Platform,
		Account:   p.Account,
		Contact:   p.Contact,
		Message:   p.Message,
		Thread:    p.Thread,
		Broadcast: p.Broadcast,
		AsUser:    p.AsUser,
		DryRun:    p.DryRun,
		SessionID: os.Getenv("PIGEON_SESSION_ID"),
	})
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", paths.SocketPath())
			},
		},
	}
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
		fmt.Printf("Dry run — would send to %s (%s) as %s", result.ChannelName, result.ChannelID, result.SendAs)
		if result.Email != "" {
			fmt.Printf(" <%s>", result.Email)
		}
		fmt.Println()
		return nil
	}

	fmt.Printf("Sent to %s at %s\n", p.Contact, result.Timestamp)
	return nil
}
