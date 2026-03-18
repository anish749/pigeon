package commands

import (
	"flag"
	"fmt"
	"os"

	"github.com/anish/claude-msg-utils/internal/config"
)

func RunSetupSlack(args []string) error {
	fs := flag.NewFlagSet("setup-slack", flag.ExitOnError)
	workspace := fs.String("workspace", "", "workspace name [required]")
	appToken := fs.String("token", "", "Slack app-level token (xapp-...) or SLACK_APP_TOKEN env var")
	botToken := fs.String("bot-token", "", "Slack bot token (xoxb-...) or SLACK_BOT_TOKEN env var")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *appToken == "" {
		*appToken = os.Getenv("SLACK_APP_TOKEN")
	}
	if *botToken == "" {
		*botToken = os.Getenv("SLACK_BOT_TOKEN")
	}

	if *workspace == "" {
		return fmt.Errorf("required flag: -workspace")
	}
	if *appToken == "" {
		return fmt.Errorf("required: -token or SLACK_APP_TOKEN env var")
	}
	if *botToken == "" {
		return fmt.Errorf("required: -bot-token or SLACK_BOT_TOKEN env var")
	}

	cfg, err := config.Load()
	if err != nil {
		cfg = &config.Config{}
	}

	cfg.AddSlack(config.SlackConfig{
		Workspace: *workspace,
		AppToken:  *appToken,
		BotToken:  *botToken,
	})

	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	fmt.Printf("Slack workspace %q saved to config: %s\n\n", *workspace, config.ConfigPath())
	fmt.Printf("Start listening with:\n")
	fmt.Printf("  cmu daemon start\n")
	return nil
}
