package commands

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
)

func RunSend(args []string) error {
	fs := flag.NewFlagSet("send", flag.ExitOnError)
	platform := fs.String("platform", "", "platform (e.g. whatsapp, slack) [required]")
	account := fs.String("account", "", "account (e.g. +14155551234, acme-corp) [required]")
	contact := fs.String("contact", "", "contact name, phone, or channel [required]")
	message := fs.String("m", "", "message text [required]")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *platform == "" || *account == "" || *contact == "" || *message == "" {
		return fmt.Errorf("required flags: -platform, -account, -contact, -m")
	}

	body, _ := json.Marshal(map[string]string{
		"platform": *platform,
		"account":  *account,
		"contact":  *contact,
		"message":  *message,
	})

	resp, err := http.Post("http://localhost:9876/api/send", "application/json", bytes.NewReader(body))
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

	fmt.Printf("Sent to %s at %s\n", *contact, result.Timestamp)
	return nil
}
