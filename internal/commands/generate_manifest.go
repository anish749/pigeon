package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/manifoldco/promptui"

	"github.com/anish749/pigeon/internal/config"
)

func RunGenerateManifest(username, workspace, appDisplayName string) error {
	if workspace == "" {
		selected, err := selectSlackWorkspace()
		if err != nil {
			return err
		}
		workspace = selected
	}

	displayName, err := resolveSlackAppDisplayName(workspace, appDisplayName)
	if err != nil {
		return err
	}

	rendered, err := renderManifest(username, workspace, displayName)
	if err != nil {
		return err
	}

	fmt.Print(rendered)

	// Copy to clipboard on macOS.
	clip := exec.Command("pbcopy")
	clip.Stdin = strings.NewReader(rendered)
	if err := clip.Run(); err == nil {
		fmt.Fprintln(os.Stderr, "\n(copied to clipboard)")
	}

	return nil
}

func resolveSlackAppDisplayName(workspace, override string) (string, error) {
	if override != "" {
		return override, nil
	}

	if name, ok, err := configuredSlackAppDisplayName(workspace); err != nil {
		return "", err
	} else if ok {
		return name, nil
	}
	return (config.SlackConfig{}).AppDisplay(), nil
}

func configuredSlackAppDisplayName(workspace string) (string, bool, error) {
	cfg, err := config.Load()
	if err != nil {
		return "", false, err
	}
	for _, sl := range cfg.Slack {
		if sl.Workspace == workspace {
			if sl.AppDisplayName != "" {
				return sl.AppDisplayName, true, nil
			}
			return "", false, nil
		}
	}
	return "", false, nil
}

// selectSlackWorkspace shows an interactive picker for configured Slack workspaces.
func selectSlackWorkspace() (string, error) {
	cfg, err := config.Load()
	if err != nil {
		return "", err
	}

	if len(cfg.Slack) == 0 {
		return "", fmt.Errorf("no Slack workspaces configured — run 'pigeon setup-slack' first")
	}

	var names []string
	for _, s := range cfg.Slack {
		names = append(names, s.Workspace)
	}

	if len(names) == 1 {
		return names[0], nil
	}

	prompt := promptui.Select{
		Label: "Select Slack workspace",
		Items: names,
		Size:  10,
	}

	idx, _, err := prompt.Run()
	if err != nil {
		return "", fmt.Errorf("selection cancelled")
	}

	return names[idx], nil
}

// renderManifest reads the manifest template and substitutes username/workspace/app display name.
func renderManifest(username, workspace, appDisplayName string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("find executable: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("resolve executable: %w", err)
	}
	tmplPath := filepath.Join(filepath.Dir(exe), "manifests", "slack-app.yaml")

	tmpl, err := os.ReadFile(tmplPath)
	if err != nil {
		return "", fmt.Errorf("read manifest template: %w", err)
	}

	return renderManifestTemplate(string(tmpl), username, workspace, appDisplayName), nil
}

func renderManifestTemplate(tmpl, username, workspace, appDisplayName string) string {
	rendered := strings.ReplaceAll(tmpl, "${USERNAME}", username)
	rendered = strings.ReplaceAll(rendered, "${WORKSPACE_NAME}", workspace)
	rendered = strings.ReplaceAll(rendered, "${APP_DISPLAY_NAME}", appDisplayName)
	return rendered
}
