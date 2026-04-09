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

	fmt.Println("Google Workspace Account Setup")
	fmt.Println("==============================")
	fmt.Println()
	fmt.Println("Pigeon uses the `gws` CLI for all Google Workspace auth.")
	fmt.Println("Whichever account `gws` is logged into is the one pigeon will poll.")
	fmt.Println()

	email, err := gwsauth.CurrentUser()
	if err != nil {
		return fmt.Errorf("probe gws auth: %w", err)
	}
	if email == "" {
		fmt.Println("  No Google account is logged into the gws CLI.")
		fmt.Println()
		fmt.Println("  Run the following, then re-run `pigeon setup-gws`:")
		fmt.Println("    gws auth login")
		return fmt.Errorf("gws is not logged in")
	}

	fmt.Printf("  Detected Google account: %s\n", email)
	fmt.Println()

	// Suggest a label from the email's local-part, title-cased.
	suggestion := defaultLabel(email)

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// If an entry for this email already exists, surface it so the user
	// can see they're updating rather than adding.
	if existing := findGWS(cfg, email); existing != nil {
		fmt.Printf("  An entry for this email already exists (label: %q).\n", existing.Account)
		fmt.Println("  Continuing will update its label.")
		fmt.Println()
		suggestion = existing.Account
	}

	fmt.Printf("  Account label [%s]: ", suggestion)
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

	fmt.Println()
	fmt.Printf("Saved %s (%q) to config.\n", email, label)
	fmt.Println()
	fmt.Println("To start polling Gmail, Calendar, and Drive:")
	fmt.Println("  pigeon daemon restart")
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
