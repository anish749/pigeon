package commands

import (
	"fmt"
	"strings"

	"github.com/manifoldco/promptui"

	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/daemon"
	gwsauth "github.com/anish749/pigeon/internal/gws/auth"
)

func RunSetupGWS(args []string) error {
	user, err := gwsauth.CurrentUser()
	if err != nil {
		return fmt.Errorf("probe gws auth: %w", err)
	}
	if user == nil {
		return fmt.Errorf("gws is not logged in — run `gws auth login` first")
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	fmt.Printf("Account: %s\n", user.Email)

	// When updating an existing entry, pre-fill the prompt with the
	// current label so <enter> keeps it.
	var currentLabel string
	if existing := findGWS(cfg, user.Email); existing != nil {
		fmt.Println("(updating existing entry)")
		currentLabel = existing.Account
	}

	prompt := promptui.Prompt{
		Label:   "Label",
		Default: currentLabel,
		Validate: func(s string) error {
			if strings.TrimSpace(s) == "" {
				return fmt.Errorf("label cannot be empty")
			}
			return nil
		},
	}
	label, err := prompt.Run()
	if err != nil {
		return fmt.Errorf("prompt cancelled: %w", err)
	}

	cfg.AddGWS(config.GWSConfig{
		Account: strings.TrimSpace(label),
		Email:   user.Email,
	})
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	// The daemon's GWSManager watches config via fsnotify and reconciles
	// on every change — so if it's already running, the new account is
	// picked up automatically. Otherwise the user needs to start it.
	if daemon.IsRunning() {
		fmt.Println("Saved. Daemon will pick up the new account automatically.")
	} else {
		fmt.Println("Saved. Run `pigeon daemon start` to begin polling.")
	}
	return nil
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
