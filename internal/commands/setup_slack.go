package commands

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"

	"github.com/anish/claude-msg-utils/internal/config"
	slacklistener "github.com/anish/claude-msg-utils/internal/listener/slack"
)

func RunSetupSlack(args []string) error {
	fs := flag.NewFlagSet("setup-slack", flag.ExitOnError)
	clientID := fs.String("client-id", "", "Slack app client ID (only needed on first setup)")
	clientSecret := fs.String("client-secret", "", "Slack app client secret (only needed on first setup)")
	appToken := fs.String("app-token", "", "Slack app-level token xapp-... (only needed on first setup)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		cfg = &config.Config{}
	}

	// First-time setup: need app credentials
	if cfg.SlackApp == nil {
		if *clientID == "" {
			*clientID = os.Getenv("SLACK_CLIENT_ID")
		}
		if *clientSecret == "" {
			*clientSecret = os.Getenv("SLACK_CLIENT_SECRET")
		}
		if *appToken == "" {
			*appToken = os.Getenv("SLACK_APP_TOKEN")
		}

		if *clientID == "" || *clientSecret == "" || *appToken == "" {
			return fmt.Errorf(`first-time setup requires:
  -client-id     (or SLACK_CLIENT_ID env var)
  -client-secret (or SLACK_CLIENT_SECRET env var)
  -app-token     (or SLACK_APP_TOKEN env var)

Step 1: Create the Slack app

  1. Go to https://api.slack.com/apps
  2. Click "Create New App" → "From a manifest" → pick any workspace
  3. Paste the contents of manifests/slack-app.yaml → Create
  4. Under "Basic Information", copy the Client ID and Client Secret
  5. Under "Socket Mode" (left sidebar), enable it and create an
     app-level token with scope "connections:write" → copy it (xapp-...)

Step 2: Run setup

  cmu setup-slack \
    -client-id=<Client ID> \
    -client-secret=<Client Secret> \
    -app-token=<xapp-...>

  HTTPS certificates for the OAuth callback will be generated
  automatically using mkcert (installed via Homebrew if needed).

Multi-workspace installation:

  After the first install, add more workspaces by running:
    cmu setup-slack

  To install in workspaces you don't own, enable distribution:
  1. Go to https://api.slack.com/apps → your app → "Manage Distribution"
  2. Click "Activate Public Distribution"
  3. Run "cmu setup-slack" — pick the new workspace in the browser`)
		}

		cfg.SlackApp = &config.SlackApp{
			ClientID:     *clientID,
			ClientSecret: *clientSecret,
			AppToken:     *appToken,
		}
		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
		fmt.Printf("Slack app credentials saved to %s\n\n", config.ConfigPath())
	}

	// Ensure TLS certs exist — install mkcert and generate if needed
	if !slacklistener.HasTLSCerts() {
		if err := ensureMkcert(); err != nil {
			return err
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := slacklistener.NewAuthServer(cfg.SlackApp.ClientID, cfg.SlackApp.ClientSecret, nil)

	// Start OAuth server in background
	go func() {
		if err := srv.Start(ctx); err != nil {
			slog.ErrorContext(ctx, "oauth server error", "error", err)
		}
	}()

	installURL := srv.InstallURL()
	fmt.Printf("Opening browser to install Slack app...\n\n")
	fmt.Printf("If the browser doesn't open, visit:\n  %s\n\n", installURL)
	fmt.Println("Waiting for OAuth callback...")

	openBrowser(installURL)

	// Wait for the callback
	entry := <-srv.Installed()
	cancel()

	fmt.Printf("\nWorkspace %q (team: %s) installed and saved to config.\n\n", entry.Workspace, entry.TeamID)
	fmt.Printf("To add another workspace, run:\n  cmu setup-slack\n\n")
	fmt.Printf("To start listening on all workspaces:\n  cmu daemon start\n")
	return nil
}

// ensureMkcert checks for mkcert, installs it if missing, and generates localhost certs.
func ensureMkcert() error {
	// Check if mkcert is installed
	if _, err := exec.LookPath("mkcert"); err != nil {
		fmt.Println("mkcert not found. Installing via Homebrew...")
		cmd := exec.Command("brew", "install", "mkcert")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to install mkcert: %w\n\nInstall it manually: brew install mkcert", err)
		}
		fmt.Println()
	}

	// Install the local CA (if not already done)
	fmt.Println("Installing mkcert CA into system trust store (may ask for password)...")
	cmd := exec.Command("mkcert", "-install")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("mkcert -install failed: %w", err)
	}
	fmt.Println()

	// Generate localhost cert
	fmt.Println("Generating HTTPS certificate for localhost...")
	cmd = exec.Command("mkcert",
		"-cert-file", slacklistener.CertPath(),
		"-key-file", slacklistener.KeyPath(),
		"localhost")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("mkcert cert generation failed: %w", err)
	}
	fmt.Println()

	return nil
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
