package commands

import (
	"fmt"
	"slices"
	"strings"

	"github.com/anish749/pigeon/internal/config"
)

// RunAutoApproveAdd adds a Slack user ID to the auto-approve list for a workspace.
func RunAutoApproveAdd(workspace, userID string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	sc, idx := findSlackConfig(cfg, workspace)
	if sc == nil {
		return fmt.Errorf("no Slack workspace %q in config", workspace)
	}

	if !strings.HasPrefix(userID, "U") {
		return fmt.Errorf("user ID must be U-prefixed (e.g. U07HF6KQ7PY), got %q", userID)
	}

	if slices.Contains(sc.AutoApprove, userID) {
		fmt.Printf("%s already auto-approved for %s\n", userID, workspace)
		return nil
	}

	cfg.Slack[idx].AutoApprove = append(cfg.Slack[idx].AutoApprove, userID)
	if err := config.Save(cfg); err != nil {
		return err
	}
	fmt.Printf("Added %s to auto-approve for %s\n", userID, workspace)
	fmt.Println("Restart the daemon to apply: pigeon daemon restart")
	return nil
}

// RunAutoApproveRemove removes a Slack user ID from the auto-approve list.
func RunAutoApproveRemove(workspace, userID string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	sc, idx := findSlackConfig(cfg, workspace)
	if sc == nil {
		return fmt.Errorf("no Slack workspace %q in config", workspace)
	}

	found := false
	filtered := sc.AutoApprove[:0]
	for _, existing := range sc.AutoApprove {
		if existing == userID {
			found = true
			continue
		}
		filtered = append(filtered, existing)
	}
	if !found {
		return fmt.Errorf("%s is not in the auto-approve list for %s", userID, workspace)
	}

	cfg.Slack[idx].AutoApprove = filtered
	if err := config.Save(cfg); err != nil {
		return err
	}
	fmt.Printf("Removed %s from auto-approve for %s\n", userID, workspace)
	fmt.Println("Restart the daemon to apply: pigeon daemon restart")
	return nil
}

// RunAutoApproveList prints the auto-approve list for a workspace.
func RunAutoApproveList(workspace string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	sc, _ := findSlackConfig(cfg, workspace)
	if sc == nil {
		return fmt.Errorf("no Slack workspace %q in config", workspace)
	}

	if len(sc.AutoApprove) == 0 {
		fmt.Println("No auto-approved users")
		return nil
	}

	for _, uid := range sc.AutoApprove {
		fmt.Println(uid)
	}
	return nil
}

// findSlackConfig returns the SlackConfig and its index for a workspace name
// (case-insensitive slug match).
func findSlackConfig(cfg *config.Config, workspace string) (*config.SlackConfig, int) {
	workspace = strings.ToLower(workspace)
	for i := range cfg.Slack {
		if strings.ToLower(cfg.Slack[i].Workspace) == workspace {
			return &cfg.Slack[i], i
		}
	}
	return nil, -1
}
