package commands

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/anish749/pigeon/internal/config"
)

// RunWorkspaceList prints all configured workspaces and their accounts.
func RunWorkspaceList(cfg *config.Config) error {
	if len(cfg.Workspaces) == 0 {
		fmt.Println("No workspaces configured.")
		return nil
	}

	names := make([]config.WorkspaceName, 0, len(cfg.Workspaces))
	for name := range cfg.Workspaces {
		names = append(names, name)
	}
	slices.SortFunc(names, func(a, b config.WorkspaceName) int {
		return cmp.Compare(a, b)
	})

	for _, name := range names {
		ws := cfg.Workspaces[name]
		marker := ""
		if name == cfg.DefaultWorkspace {
			marker = " (default)"
		}
		fmt.Printf("%s%s\n", name, marker)
		for _, slug := range ws.Slack {
			fmt.Printf("  slack/%s\n", slug)
		}
		for _, slug := range ws.GWS {
			fmt.Printf("  gws/%s\n", slug)
		}
		for _, slug := range ws.WhatsApp {
			fmt.Printf("  whatsapp/%s\n", slug)
		}
	}
	return nil
}

// RunWorkspaceAdd adds an account to a workspace, creating the workspace if it
// doesn't exist. The account must exist in the top-level config.
func RunWorkspaceAdd(cfg *config.Config, workspace, platform, account string) error {
	if err := validateAccountExists(cfg, platform, account); err != nil {
		return err
	}

	if cfg.Workspaces == nil {
		cfg.Workspaces = make(map[config.WorkspaceName]config.WorkspaceConfig)
	}

	ws := cfg.Workspaces[config.WorkspaceName(workspace)]

	switch platform {
	case "slack":
		if slices.Contains(ws.Slack, account) {
			return fmt.Errorf("slack/%s already in workspace %q", account, workspace)
		}
		ws.Slack = append(ws.Slack, account)
	case "gws":
		if slices.Contains(ws.GWS, account) {
			return fmt.Errorf("gws/%s already in workspace %q", account, workspace)
		}
		ws.GWS = append(ws.GWS, account)
	case "whatsapp":
		if slices.Contains(ws.WhatsApp, account) {
			return fmt.Errorf("whatsapp/%s already in workspace %q", account, workspace)
		}
		ws.WhatsApp = append(ws.WhatsApp, account)
	default:
		return fmt.Errorf("unsupported platform %q (supported: slack, gws, whatsapp)", platform)
	}

	cfg.Workspaces[config.WorkspaceName(workspace)] = ws
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	fmt.Printf("Added %s/%s to workspace %q.\n", platform, account, workspace)
	return nil
}

// RunWorkspaceRemove removes an account from a workspace. If the workspace
// becomes empty, it is deleted.
func RunWorkspaceRemove(cfg *config.Config, workspace, platform, account string) error {
	ws, ok := cfg.Workspaces[config.WorkspaceName(workspace)]
	if !ok {
		return fmt.Errorf("workspace %q not found", workspace)
	}

	lenBefore := len(ws.Slack) + len(ws.GWS) + len(ws.WhatsApp)
	match := func(v string) bool { return v == account }
	switch platform {
	case "slack":
		ws.Slack = slices.DeleteFunc(ws.Slack, match)
	case "gws":
		ws.GWS = slices.DeleteFunc(ws.GWS, match)
	case "whatsapp":
		ws.WhatsApp = slices.DeleteFunc(ws.WhatsApp, match)
	default:
		return fmt.Errorf("unsupported platform %q (supported: slack, gws, whatsapp)", platform)
	}
	if len(ws.Slack)+len(ws.GWS)+len(ws.WhatsApp) == lenBefore {
		return fmt.Errorf("%s/%s not in workspace %q", platform, account, workspace)
	}

	if len(ws.Slack)+len(ws.GWS)+len(ws.WhatsApp) == 0 {
		delete(cfg.Workspaces, config.WorkspaceName(workspace))
		if cfg.DefaultWorkspace == config.WorkspaceName(workspace) {
			cfg.DefaultWorkspace = ""
		}
		fmt.Printf("Workspace %q is now empty — deleted.\n", workspace)
	} else {
		cfg.Workspaces[config.WorkspaceName(workspace)] = ws
		fmt.Printf("Removed %s/%s from workspace %q.\n", platform, account, workspace)
	}

	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	return nil
}

// RunWorkspaceDelete deletes an entire workspace from config.
func RunWorkspaceDelete(cfg *config.Config, workspace string) error {
	if _, ok := cfg.Workspaces[config.WorkspaceName(workspace)]; !ok {
		return fmt.Errorf("workspace %q not found", workspace)
	}
	delete(cfg.Workspaces, config.WorkspaceName(workspace))
	if cfg.DefaultWorkspace == config.WorkspaceName(workspace) {
		cfg.DefaultWorkspace = ""
	}
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	fmt.Printf("Deleted workspace %q.\n", workspace)
	return nil
}

// RunWorkspaceDefault shows or sets the default workspace.
func RunWorkspaceDefault(cfg *config.Config, workspace string) error {
	if workspace == "" {
		if cfg.DefaultWorkspace == "" {
			fmt.Println("No default workspace set.")
		} else {
			fmt.Println(cfg.DefaultWorkspace)
		}
		return nil
	}

	if _, ok := cfg.Workspaces[config.WorkspaceName(workspace)]; !ok {
		return fmt.Errorf("workspace %q not found", workspace)
	}
	cfg.DefaultWorkspace = config.WorkspaceName(workspace)
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	fmt.Printf("Default workspace set to %q.\n", workspace)
	return nil
}

// validateAccountExists checks that the given platform/account is configured.
func validateAccountExists(cfg *config.Config, platform, account string) error {
	var configured []string
	switch platform {
	case "slack":
		for _, s := range cfg.Slack {
			if s.Workspace == account {
				return nil
			}
			configured = append(configured, s.Workspace)
		}
	case "gws":
		for _, g := range cfg.GWS {
			if g.Email == account {
				return nil
			}
			configured = append(configured, g.Email)
		}
	case "whatsapp":
		for _, w := range cfg.WhatsApp {
			if w.Account == account {
				return nil
			}
			configured = append(configured, w.Account)
		}
	default:
		return fmt.Errorf("unsupported platform %q (supported: slack, gws, whatsapp)", platform)
	}
	if len(configured) == 0 {
		return fmt.Errorf("no %s accounts configured", platform)
	}
	return fmt.Errorf("%s account %q not found in config (configured: %s)",
		platform, account, strings.Join(configured, ", "))
}
