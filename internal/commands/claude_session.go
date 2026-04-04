package commands

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/manifoldco/promptui"

	"github.com/anish/claude-msg-utils/internal/claude"
	"github.com/anish/claude-msg-utils/internal/config"
)

// ANSI color helpers.
const (
	bold   = "\033[1m"
	dim    = "\033[2m"
	yellow = "\033[33m"
	green  = "\033[32m"
	cyan   = "\033[36m"
	reset  = "\033[0m"
)

type ClaudeSessionParams struct {
	Platform string
	Account  string
}

func RunClaudeSession(p ClaudeSessionParams) error {
	platform := strings.ToLower(p.Platform)
	account := p.Account

	// If platform+account not provided, let the user choose interactively.
	if platform == "" || account == "" {
		var err error
		platform, account, err = selectAccount()
		if err != nil {
			return err
		}
	} else {
		// Validate that the provided platform+account exist in config.
		if err := validateAccount(platform, account); err != nil {
			return err
		}
	}

	name := claude.SessionName(platform, account)

	sf, err := claude.OpenSession(platform, account)
	if err != nil {
		return err
	}
	defer sf.Close()

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	if sf.Exists() {
		return handleExistingSession(sf, cwd)
	}

	return handleNewSession(sf, platform, account, name, cwd)
}

// accountOption represents a selectable platform+account pair.
type accountOption struct {
	Platform string
	Account  string
	Label    string // e.g. "slack / tubular"
}

func selectAccount() (platform, account string, err error) {
	cfg, err := config.Load()
	if err != nil {
		return "", "", err
	}

	var options []accountOption
	for _, s := range cfg.Slack {
		options = append(options, accountOption{
			Platform: "slack",
			Account:  s.Workspace,
			Label:    fmt.Sprintf("slack / %s", s.Workspace),
		})
	}
	for _, w := range cfg.WhatsApp {
		options = append(options, accountOption{
			Platform: "whatsapp",
			Account:  w.Account,
			Label:    fmt.Sprintf("whatsapp / %s", w.Account),
		})
	}

	if len(options) == 0 {
		return "", "", fmt.Errorf("no accounts configured — run 'pigeon setup-slack' or 'pigeon setup-whatsapp' first")
	}

	var labels []string
	for _, o := range options {
		labels = append(labels, o.Label)
	}

	prompt := promptui.Select{
		Label: "Select account for Claude session",
		Items: labels,
		Size:  10,
	}

	idx, _, err := prompt.Run()
	if err != nil {
		return "", "", fmt.Errorf("selection cancelled")
	}

	selected := options[idx]
	return selected.Platform, selected.Account, nil
}

func handleExistingSession(sf *claude.SessionFile, cwd string) error {
	s := sf.Data()
	fmt.Printf("\n%sFound existing session for %s%s%s\n", bold, cyan, s.Name, reset)
	fmt.Printf("  Session ID:  %s%s%s\n", dim, s.SessionID, reset)
	fmt.Printf("  Directory:   %s%s%s\n", dim, s.CWD, reset)
	fmt.Printf("  Created:     %s%s%s\n\n", dim, s.CreatedAt.Format("2006-01-02 15:04"), reset)

	if cwd != s.CWD {
		fmt.Printf("%s⚠  Your current directory is %s%s\n", yellow, cwd, reset)
		fmt.Printf("%s   The existing session is in %s%s\n", yellow, s.CWD, reset)
		fmt.Printf("%s   To continue, your working directory will be changed to %s%s\n\n", yellow, s.CWD, reset)
	}

	if !confirm("Continue with this session?", true) {
		fmt.Println()
		return handleNewSession(sf, s.Platform, s.Account, s.Name, cwd)
	}

	// Resume in the stored cwd.
	return launchClaude(s.SessionID, s.Name, s.CWD, true)
}

func handleNewSession(sf *claude.SessionFile, platform, account, name, cwd string) error {
	fmt.Printf("\n%sCreating new Claude Code session for %s%s%s\n\n", bold, cyan, name, reset)
	fmt.Printf("  Working directory: %s%s%s\n\n", bold, cwd, reset)
	fmt.Printf("  %s⚠  Everything in this directory will be accessible to the pigeon bot%s\n", yellow, reset)
	fmt.Printf("  %s   and may be exposed to others talking to pigeon over %s in %s.%s\n\n", yellow, platform, account, reset)

	if !confirm("Continue?", true) {
		fmt.Println("Aborted.")
		return nil
	}

	sessionID := uuid.New().String()
	now := time.Now()

	s := &claude.Session{
		Platform:      platform,
		Account:       account,
		SessionID:     sessionID,
		CWD:           cwd,
		Name:          name,
		CreatedAt:     now,
		LastDelivered: now, // Only messages arriving after session creation will be delivered.
	}

	if err := sf.Save(s); err != nil {
		return err
	}

	fmt.Printf("\n  %s✓%s Session created\n", green, reset)
	fmt.Printf("  Session ID:   %s%s%s\n", dim, sessionID, reset)
	fmt.Printf("  Session file: %s%s%s\n\n", dim, claude.SessionPath(platform, account), reset)

	return launchClaude(sessionID, name, cwd, false)
}

func launchClaude(sessionID, name, cwd string, resume bool) error {
	if err := os.Chdir(cwd); err != nil {
		return fmt.Errorf("change to directory %s: %w", cwd, err)
	}

	claudePath, err := findClaude()
	if err != nil {
		return err
	}

	var args []string
	if resume {
		fmt.Printf("  %sResuming Claude Code session...%s\n\n", dim, reset)
		args = []string{
			"claude",
			"--resume", sessionID,
			"--dangerously-load-development-channels", "server:pigeon",
		}
	} else {
		fmt.Printf("  %sStarting Claude Code session...%s\n\n", dim, reset)
		args = []string{
			"claude",
			"--session-id", sessionID,
			"--name", name,
			"--dangerously-load-development-channels", "server:pigeon",
		}
	}

	// Pass session ID to claude (and its MCP shim) via environment.
	env := append(os.Environ(), "PIGEON_SESSION_ID="+sessionID)

	// Exec replaces this process with claude.
	return syscall.Exec(claudePath, args, env)
}

func findClaude() (string, error) {
	pathDirs := strings.Split(os.Getenv("PATH"), ":")
	for _, dir := range pathDirs {
		full := dir + "/claude"
		if info, err := os.Stat(full); err == nil && !info.IsDir() {
			return full, nil
		}
	}
	return "", fmt.Errorf("claude not found in PATH — install Claude Code first")
}

func validateAccount(platform, account string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	switch platform {
	case "slack":
		for _, s := range cfg.Slack {
			if strings.EqualFold(s.Workspace, account) {
				return nil
			}
		}
		return fmt.Errorf("no Slack workspace %q found in config — run 'pigeon setup-slack' first", account)
	case "whatsapp":
		for _, w := range cfg.WhatsApp {
			if strings.EqualFold(w.Account, account) {
				return nil
			}
		}
		return fmt.Errorf("no WhatsApp account %q found in config — run 'pigeon setup-whatsapp' first", account)
	default:
		return fmt.Errorf("unsupported platform: %s (supported: slack, whatsapp)", platform)
	}
}

func confirm(prompt string, defaultYes bool) bool {
	hint := "Y/n"
	if !defaultYes {
		hint = "y/N"
	}

	fmt.Printf("%s%s [%s]:%s ", bold, prompt, hint, reset)

	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return defaultYes
	}

	answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
	if answer == "" {
		return defaultYes
	}
	return answer == "y" || answer == "yes"
}
