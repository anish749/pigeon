package commands

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"strings"

	"github.com/anish749/pigeon/internal/config"
	slacklistener "github.com/anish749/pigeon/internal/listener/slack"
)

func RunSetupSlack(args []string) error {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("Slack Workspace Setup")
	fmt.Println("=====================")
	fmt.Println()
	fmt.Println("Each workspace needs its own internal Slack app.")
	fmt.Println()

	// Prompt for username with a suggestion from the OS
	suggestion := suggestUsername()
	if suggestion != "" {
		fmt.Printf("  Your name [%s]: ", suggestion)
	} else {
		fmt.Print("  Your name: ")
	}
	username, _ := reader.ReadString('\n')
	username = strings.TrimSpace(username)
	if username == "" {
		if suggestion != "" {
			username = suggestion
		} else {
			return fmt.Errorf("name is required")
		}
	}

	// Prompt for workspace name
	fmt.Print("  Workspace name (e.g. acme-corp): ")
	workspace, _ := reader.ReadString('\n')
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return fmt.Errorf("workspace name is required")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	existing := lookupSlackConfig(cfg, workspace)
	fmt.Printf("  Slack app display name [%s]: ", existing.AppDisplay())
	appDisplayName, _ := reader.ReadString('\n')
	appDisplayName = strings.TrimSpace(appDisplayName)
	if appDisplayName == "" {
		appDisplayName = existing.AppDisplayName
	}
	fmt.Println()

	// Render manifest with user's values and copy to clipboard
	displayName := config.SlackConfig{AppDisplayName: appDisplayName}.AppDisplay()
	rendered, err := renderManifest(username, workspace, displayName)
	if err != nil {
		fmt.Printf("  (Could not render manifest: %v)\n", err)
		fmt.Println("  Run `pigeon generate-manifest` manually to create the manifest.")
		return err
	}
	clip := exec.Command("pbcopy")
	clip.Stdin = strings.NewReader(rendered)
	if err := clip.Run(); err != nil {
		fmt.Printf("  (Could not copy manifest to clipboard: %v)\n", err)
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

	srv := slacklistener.NewAuthServer(clientID, clientSecret, appToken, appDisplayName)

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

// suggestUsername returns a name suggestion from the OS user's display name,
// or from an existing Slack config if available.
func suggestUsername() string {
	u, err := user.Current()
	if err != nil {
		return ""
	}
	// On macOS, Name is the full display name (e.g. "Anish Sharma")
	if u.Name != "" {
		// Use first name only
		if first, _, ok := strings.Cut(u.Name, " "); ok {
			return first
		}
		return u.Name
	}
	return u.Username
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
