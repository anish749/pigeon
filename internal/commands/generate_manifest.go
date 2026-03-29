package commands

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func RunGenerateManifest(args []string) error {
	fs := flag.NewFlagSet("generate-manifest", flag.ExitOnError)
	username := fs.String("username", "", "display name for the bot owner")
	workspace := fs.String("workspace", "", "Slack workspace name")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *username == "" || *workspace == "" {
		return fmt.Errorf("both -username and -workspace are required")
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	tmplPath := filepath.Join(filepath.Dir(exe), "manifests", "slack-app.yaml")

	tmpl, err := os.ReadFile(tmplPath)
	if err != nil {
		return fmt.Errorf("read manifest template: %w", err)
	}

	rendered := strings.ReplaceAll(string(tmpl), "${USERNAME}", *username)
	rendered = strings.ReplaceAll(rendered, "${WORKSPACE_NAME}", *workspace)

	fmt.Print(rendered)

	// Copy to clipboard on macOS.
	clip := exec.Command("pbcopy")
	clip.Stdin = strings.NewReader(rendered)
	if err := clip.Run(); err == nil {
		fmt.Fprintln(os.Stderr, "\n(copied to clipboard)")
	}

	return nil
}
