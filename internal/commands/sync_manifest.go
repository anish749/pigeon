package commands

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/anish/claude-msg-utils/internal/config"
)

func RunSyncManifest(args []string) error {
	fs := flag.NewFlagSet("sync-manifest", flag.ExitOnError)
	username := fs.String("username", "", "display name for the bot owner")
	workspace := fs.String("workspace", "", "Slack workspace (default: all configured)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *username == "" {
		return fmt.Errorf("-username is required")
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if *workspace != "" {
		return syncWorkspace(cfg, *username, *workspace)
	}

	// No workspace specified: sync all configured workspaces.
	if len(cfg.Slack) == 0 {
		return fmt.Errorf("no Slack workspaces configured; use -workspace to create a new app")
	}

	for _, sc := range cfg.Slack {
		fmt.Printf("\n=== %s ===\n", sc.Workspace)
		if err := syncWorkspace(cfg, *username, sc.Workspace); err != nil {
			return fmt.Errorf("workspace %s: %w", sc.Workspace, err)
		}
	}
	return nil
}

func syncWorkspace(cfg *config.Config, username, workspace string) error {
	manifest, err := renderManifest(username, workspace)
	if err != nil {
		return err
	}

	if workspaceInConfig(cfg, workspace) {
		return updateAndInstall(manifest, workspace)
	}
	return createApp(manifest, workspace)
}

func workspaceInConfig(cfg *config.Config, workspace string) bool {
	for _, sc := range cfg.Slack {
		if sc.Workspace == workspace {
			return true
		}
	}
	return false
}

func renderManifest(username, workspace string) (string, error) {
	tmpl, err := os.ReadFile("manifests/slack-app.yaml")
	if err != nil {
		return "", fmt.Errorf("read manifest template: %w", err)
	}
	rendered := strings.ReplaceAll(string(tmpl), "${USERNAME}", username)
	rendered = strings.ReplaceAll(rendered, "${WORKSPACE_NAME}", workspace)
	return rendered, nil
}

func writeTempManifest(content string) (path string, cleanup func(), err error) {
	tmp, err := os.CreateTemp("", "pigeon-manifest-*.yaml")
	if err != nil {
		return "", nil, fmt.Errorf("create temp file: %w", err)
	}
	cleanup = func() { os.Remove(tmp.Name()) }

	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		cleanup()
		return "", nil, fmt.Errorf("write temp manifest: %w", err)
	}
	tmp.Close()
	return tmp.Name(), cleanup, nil
}

func updateAndInstall(manifest, workspace string) error {
	path, cleanup, err := writeTempManifest(manifest)
	if err != nil {
		return err
	}
	defer cleanup()

	fmt.Println("Updating manifest...")
	if err := slackCLI("manifest", "update", "--manifest", path); err != nil {
		return fmt.Errorf("slack manifest update: %w", err)
	}

	fmt.Printf("Reinstalling app in %s...\n", workspace)
	if err := slackCLI("install", "--workspace", workspace); err != nil {
		return fmt.Errorf("slack install: %w", err)
	}

	fmt.Println("Done!")
	return nil
}

func createApp(manifest, workspace string) error {
	path, cleanup, err := writeTempManifest(manifest)
	if err != nil {
		return err
	}
	defer cleanup()

	fmt.Printf("Creating new Slack app for %s...\n", workspace)
	if err := slackCLI("manifest", "create", "--manifest", path); err != nil {
		return fmt.Errorf("slack manifest create: %w", err)
	}

	fmt.Println("\nApp created! Next steps:")
	fmt.Println("  1. Enable Socket Mode and create an app-level token (xapp-...)")
	fmt.Println("  2. Run: pigeon setup-slack -client-id=... -client-secret=... -app-token=...")
	fmt.Println("  3. Approve the OAuth prompt to install in the workspace")
	return nil
}

func slackCLI(args ...string) error {
	cmd := exec.Command("slack", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}
