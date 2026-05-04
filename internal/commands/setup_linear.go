package commands

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/manifoldco/promptui"

	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/daemon"
	"github.com/anish749/pigeon/internal/platform/linear/auth"
)

func RunSetupLinear(args []string) error {
	creds, err := auth.Load()
	if err != nil {
		return err
	}
	if len(creds.Workspaces) == 0 {
		return fmt.Errorf("no Linear workspaces authenticated — run `linear auth login` first")
	}

	workspace, err := chooseWorkspace(creds)
	if err != nil {
		return err
	}

	if err := verifyLinearWorkspace(workspace); err != nil {
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

// chooseWorkspace returns the only authenticated workspace when there is
// one, or prompts the user to pick from the full list. The default workspace
// (per the linear CLI) is highlighted and pre-selected.
func chooseWorkspace(creds *auth.Credentials) (string, error) {
	if len(creds.Workspaces) == 1 {
		return creds.Workspaces[0], nil
	}

	labels := make([]string, len(creds.Workspaces))
	cursor := 0
	for i, w := range creds.Workspaces {
		if w == creds.Default {
			labels[i] = w + " (default)"
			cursor = i
		} else {
			labels[i] = w
		}
	}

	prompt := promptui.Select{
		Label:     "Select Linear workspace",
		Items:     labels,
		CursorPos: cursor,
	}
	idx, _, err := prompt.Run()
	if err != nil {
		return "", fmt.Errorf("selection cancelled: %w", err)
	}
	return creds.Workspaces[idx], nil
}

// verifyLinearWorkspace confirms the linear CLI can talk to the chosen
// workspace by issuing a no-op GraphQL query through `linear --workspace`.
// Passing the slug explicitly avoids touching the CLI's stored default.
func verifyLinearWorkspace(workspace string) error {
	out, err := exec.Command("linear", "--workspace", workspace, "api", `{ viewer { organization { urlKey } } }`).Output()
	if err != nil {
		return fmt.Errorf("verify linear workspace %q: %w (run `linear auth login`?)", workspace, err)
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
		return fmt.Errorf("parse linear api response for %q: %w", workspace, err)
	}
	if got := result.Data.Viewer.Organization.URLKey; got != workspace {
		return fmt.Errorf("linear returned urlKey %q, expected %q", got, workspace)
	}
	return nil
}

func findLinear(cfg *config.Config, workspace string) *config.LinearConfig {
	for i := range cfg.Linear {
		if cfg.Linear[i].Workspace == workspace {
			return &cfg.Linear[i]
		}
	}
	return nil
}
