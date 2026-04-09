package commands

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/anish749/pigeon/internal/config"
	gwsauth "github.com/anish749/pigeon/internal/gws/auth"
)

func RunSetupGWS(args []string) error {
	reader := bufio.NewReader(os.Stdin)

	email, err := gwsauth.CurrentUser()
	if err != nil {
		return fmt.Errorf("probe gws auth: %w", err)
	}
	if email == "" {
		return fmt.Errorf("gws is not logged in — run `gws auth login` first")
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	suggestion := defaultLabel(email)
	if existing := findGWS(cfg, email); existing != nil {
		fmt.Printf("Updating existing entry for %s.\n", email)
		suggestion = existing.Account
	} else {
		fmt.Printf("Account: %s\n", email)
	}

	fmt.Printf("Label [%s]: ", suggestion)
	label, _ := reader.ReadString('\n')
	label = strings.TrimSpace(label)
	if label == "" {
		label = suggestion
	}

	cfg.AddGWS(config.GWSConfig{
		Account: label,
		Email:   email,
	})
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	fmt.Printf("Saved. Run `pigeon daemon restart` to start polling.\n")
	return nil
}

// defaultLabel derives a friendly label from an email address by taking
// the local-part, replacing dots and dashes with spaces, and title-casing
// each word. "first.last@example.com" → "First Last".
func defaultLabel(email string) string {
	local, _, ok := strings.Cut(email, "@")
	if !ok || local == "" {
		return email
	}
	local = strings.NewReplacer(".", " ", "-", " ", "_", " ").Replace(local)
	parts := strings.Fields(local)
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}

// findGWS returns the existing GWS config entry for the given email, or nil.
func findGWS(cfg *config.Config, email string) *config.GWSConfig {
	for i := range cfg.GWS {
		if cfg.GWS[i].Email == email {
			return &cfg.GWS[i]
		}
	}
	return nil
}
