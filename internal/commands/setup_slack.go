package commands

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	slacklistener "github.com/anish/claude-msg-utils/internal/listener/slack"
)

func RunSetupSlack(args []string) error {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("Slack Workspace Setup")
	fmt.Println("=====================")
	fmt.Println()
	fmt.Println("Each workspace needs its own internal Slack app.")
	fmt.Println()

	// Copy manifest to clipboard and open Slack app creation page
	if err := copyManifestToClipboard(); err != nil {
		fmt.Printf("  (Could not copy manifest to clipboard: %v)\n", err)
		fmt.Println("  Manually copy the contents of manifests/slack-app.yaml")
	} else {
		fmt.Println("  The app manifest has been copied to your clipboard.")
	}

	fmt.Println("  Opening the Slack app creation page...")
	fmt.Println()
	openBrowser("https://api.slack.com/apps?new_app=1")

	fmt.Println("  1. Click \"From a manifest\"")
	fmt.Println("  2. Pick the target workspace")
	fmt.Println("  3. Paste the manifest from your clipboard → Create")
	fmt.Println()
	fmt.Println("Once the app is created:")
	fmt.Println()

	// Client ID
	fmt.Println("  Go to \"Basic Information\" and copy the Client ID.")
	fmt.Print("  Client ID: ")
	clientID, _ := reader.ReadString('\n')
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		return fmt.Errorf("client ID is required")
	}

	// Client Secret
	fmt.Println()
	fmt.Println("  On the same page, copy the Client Secret.")
	fmt.Print("  Client Secret: ")
	clientSecret, _ := reader.ReadString('\n')
	clientSecret = strings.TrimSpace(clientSecret)
	if clientSecret == "" {
		return fmt.Errorf("client secret is required")
	}

	// App Token
	fmt.Println()
	fmt.Println("  Scroll down to \"App-Level Tokens\" and create a token")
	fmt.Println("  with scope \"connections:write\" (or copy an existing one).")
	fmt.Print("  App Token (xapp-...): ")
	appToken, _ := reader.ReadString('\n')
	appToken = strings.TrimSpace(appToken)
	if appToken == "" {
		return fmt.Errorf("app token is required")
	}

	fmt.Println()
	fmt.Println("Now let's install the app in the workspace.")
	fmt.Println("Your browser will open — approve the installation and you're done.")
	fmt.Println()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := slacklistener.NewAuthServer(clientID, clientSecret, appToken, nil)

	go func() {
		if err := srv.Start(ctx); err != nil {
			slog.ErrorContext(ctx, "oauth server error", "error", err)
		}
	}()

	installURL := srv.InstallURL()
	fmt.Printf("If the browser doesn't open, visit:\n  %s\n\n", installURL)

	openBrowser(installURL)

	entry := <-srv.Installed()
	cancel()

	fmt.Printf("\nWorkspace %q (team: %s) installed and saved to config.\n\n", entry.Workspace, entry.TeamID)
	fmt.Printf("To start listening:\n  pigeon daemon start\n")
	return nil
}

func copyManifestToClipboard() error {
	// Find manifest relative to the binary's directory
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	// Resolve symlinks to get the real path
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return err
	}
	manifestPath := filepath.Join(filepath.Dir(exe), "manifests", "slack-app.yaml")

	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}

	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(string(data))
	return cmd.Run()
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		return
	}
	cmd.Start()
}
