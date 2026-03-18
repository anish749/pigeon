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

To create the Slack app:

  1. Go to https://api.slack.com/apps
  2. Click "Create New App" → "From a manifest" → pick any workspace
  3. Paste the contents of manifests/slack-app.yaml → Create
  4. Under "Basic Information", copy the Client ID and Client Secret
  5. Under "Socket Mode" (left sidebar), enable it and create an
     app-level token with scope "connections:write" → copy it (xapp-...)

Then run:

  cmu setup-slack \
    -client-id=<Client ID> \
    -client-secret=<Client Secret> \
    -app-token=<xapp-...>

To install in additional workspaces:

  The app can be installed in any workspace you have admin access to.
  To install in workspaces you don't own, enable distribution:

  1. Go to https://api.slack.com/apps → your app → "Manage Distribution"
  2. Under "Share Your App with Other Workspaces", click "Activate Public Distribution"
     (you may need to check off the required steps first)

  After that, just run "cmu setup-slack" again — each run installs into
  one more workspace. All workspaces are saved to config and started
  together with "cmu daemon start".`)
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
