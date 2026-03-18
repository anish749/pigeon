package commands

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
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
	fmt.Println("If you haven't created one yet, follow these steps:")
	fmt.Println()
	fmt.Println("  1. Go to https://api.slack.com/apps")
	fmt.Println("  2. Click \"Create New App\" → \"From a manifest\"")
	fmt.Println("  3. Pick the target workspace")
	fmt.Println("  4. Paste the contents of manifests/slack-app.yaml → Create")
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
