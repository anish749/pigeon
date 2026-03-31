package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func RunGenerateManifest(username, workspace string) error {
	rendered, err := renderManifest(username, workspace)
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

// renderManifest reads the manifest template and substitutes username/workspace.
func renderManifest(username, workspace string) (string, error) {
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

	rendered := strings.ReplaceAll(string(tmpl), "${USERNAME}", username)
	rendered = strings.ReplaceAll(rendered, "${WORKSPACE_NAME}", workspace)
	return rendered, nil
}
