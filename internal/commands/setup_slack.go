package commands

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"strings"
	"time"

	"github.com/anish/claude-msg-utils/internal/config"
	slacklistener "github.com/anish/claude-msg-utils/internal/listener/slack"
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
	fmt.Println()

	// Render manifest with user's values and copy to clipboard
	rendered, err := renderManifest(username, workspace)
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

	if daemonRunning() {
		return setupViaDaemon(clientID, clientSecret, appToken)
	}
	return setupStandalone(clientID, clientSecret, appToken)
}

func setupStandalone(clientID, clientSecret, appToken string) error {
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

func setupViaDaemon(clientID, clientSecret, appToken string) error {
	fmt.Println("  Daemon is running — using it for OAuth.")
	fmt.Println()

	// POST credentials to the daemon's /slack/setup endpoint.
	body, _ := json.Marshal(map[string]string{
		"client_id":     clientID,
		"client_secret": clientSecret,
		"app_token":     appToken,
	})
	resp, err := http.Post("http://localhost:9876/slack/setup", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("could not reach daemon: %w", err)
	}
	defer resp.Body.Close()

	var setupResp struct {
		InstallURL string `json:"install_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&setupResp); err != nil {
		return fmt.Errorf("invalid response from daemon: %w", err)
	}

	fmt.Printf("If the browser doesn't open, visit:\n  %s\n\n", setupResp.InstallURL)
	openBrowser(setupResp.InstallURL)

	// Wait for the daemon to complete the OAuth flow (long-poll).
	client := &http.Client{Timeout: 5 * time.Minute}
	waitResp, err := client.Get("http://localhost:9876/slack/setup/wait")
	if err != nil {
		return fmt.Errorf("failed waiting for OAuth: %w", err)
	}
	defer waitResp.Body.Close()

	var entry config.SlackConfig
	if err := json.NewDecoder(waitResp.Body).Decode(&entry); err != nil {
		return fmt.Errorf("invalid OAuth result from daemon: %w", err)
	}

	fmt.Printf("\nWorkspace %q (team: %s) installed and saved to config.\n", entry.Workspace, entry.TeamID)
	fmt.Println("The daemon is already listening on the new workspace.")
	return nil
}

func daemonRunning() bool {
	conn, err := net.DialTimeout("tcp", "localhost:9876", time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
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
