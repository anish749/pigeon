package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/anish/claude-msg-utils/internal/api"
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
	body, _ := json.Marshal(api.SendRequest{
		Platform:  p.Platform,
		Account:   p.Account,
		Contact:   p.Contact,
		Message:   p.Message,
		Thread:    p.Thread,
		Broadcast: p.Broadcast,
		AsUser:    p.AsUser,
		DryRun:    p.DryRun,
	})

	resp, err := http.Post(fmt.Sprintf("http://localhost:%d/api/send", api.Port), "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("daemon not reachable (is 'pigeon daemon start' running?): %w", err)
	}
	defer resp.Body.Close()

	var result api.SendResponse
	data, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(data, &result); err != nil {
		return fmt.Errorf("unexpected response: %s", string(data))
	}

	if !result.OK {
		return fmt.Errorf("%s", result.Error)
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
