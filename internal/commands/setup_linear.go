package commands

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/manifoldco/promptui"

	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/daemon"
)

func RunSetupLinear(args []string) error {
	// Verify linear CLI is installed and authenticated by querying for teams.
	workspace, err := detectLinearWorkspace()
	if err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	fmt.Printf("Workspace: %s\n", workspace)

	var currentLabel string
	if existing := findLinear(cfg, workspace); existing != nil {
		fmt.Println("(updating existing entry)")
		currentLabel = existing.Account
	} else {
		currentLabel = workspace
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

	cfg.AddLinear(config.LinearConfig{
		Workspace: workspace,
		Account:   strings.TrimSpace(label),
	})
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	if daemon.IsRunning() {
		fmt.Println("Saved. Daemon will pick up the workspace automatically.")
	} else {
		fmt.Println("Saved. Run `pigeon daemon start` to begin polling.")
	}
	return nil
}

// detectLinearWorkspace probes the linear CLI to discover the current workspace slug.
func detectLinearWorkspace() (string, error) {
	out, err := exec.Command("linear", "api", `{ viewer { organization { urlKey } } }`).Output()
	if err != nil {
		return "", fmt.Errorf("linear CLI not available or not authenticated — run `linear auth login` first: %w", err)
	}

	var result struct {
		Data struct {
			Viewer struct {
				Organization struct {
					URLKey string `json:"urlKey"`
				} `json:"organization"`
			} `json:"viewer"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return "", fmt.Errorf("parse linear api response: %w", err)
	}
	slug := result.Data.Viewer.Organization.URLKey
	if slug == "" {
		return "", fmt.Errorf("could not determine workspace — is your linear CLI authenticated?")
	}
	return slug, nil
}

func findLinear(cfg *config.Config, workspace string) *config.LinearConfig {
	for i := range cfg.Linear {
		if cfg.Linear[i].Workspace == workspace {
			return &cfg.Linear[i]
		}
	}
	return nil
}
