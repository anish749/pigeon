package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/anish/claude-msg-utils/internal/api"
)

func RunSend(platform, account, contact, message string) error {
	body, _ := json.Marshal(map[string]string{
		"platform": platform,
		"account":  account,
		"contact":  contact,
		"message":  message,
	})

	resp, err := http.Post(fmt.Sprintf("http://localhost:%d/api/send", api.Port), "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("daemon not reachable (is 'pigeon daemon start' running?): %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		OK        bool   `json:"ok"`
		Timestamp string `json:"timestamp"`
		Error     string `json:"error"`
	}
	data, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(data, &result); err != nil {
		return fmt.Errorf("unexpected response: %s", string(data))
	}

	if !result.OK {
		return fmt.Errorf("%s", result.Error)
	}

	fmt.Printf("Sent to %s at %s\n", contact, result.Timestamp)
	return nil
}
