package commands

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/anish/claude-msg-utils/internal/api"
	"github.com/anish/claude-msg-utils/internal/config"
	"github.com/anish/claude-msg-utils/internal/daemon"
	"github.com/anish/claude-msg-utils/internal/hub"
	"github.com/anish/claude-msg-utils/internal/paths"
)

func DaemonStart() error {
	if err := daemon.Start(); err != nil {
		return err
	}
	fmt.Println("Daemon started.")
	return nil
}

func DaemonStop() error {
	if err := daemon.Stop(); err != nil {
		return err
	}
	fmt.Println("Daemon stopped.")
	return nil
}

func DaemonStatus() error {
	running, pid := daemon.Status()
	if running {
		fmt.Printf("Running (pid=%d, log=%s)\n", pid, paths.LogPath())
	} else {
		fmt.Println("Not running.")
	}
	return nil
}

func DaemonRestart() error {
	if daemon.IsRunning() {
		if err := daemon.Stop(); err != nil {
			return err
		}
	}
	if err := daemon.Start(); err != nil {
		return err
	}
	fmt.Println("Daemon restarted.")
	return nil
}

// DaemonRun is the actual daemon process, invoked via "daemon _run".
func DaemonRun() error {
	logWriter := &lumberjack.Logger{
		Filename:   paths.LogPath(),
		MaxSize:    10, // megabytes
		MaxBackups: 2,
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(logWriter, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	if err := daemon.WritePID(); err != nil {
		return fmt.Errorf("write PID file: %w", err)
	}
	defer daemon.RemovePID()

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if len(cfg.WhatsApp) == 0 && len(cfg.Slack) == 0 {
		return fmt.Errorf("no listeners configured in %s\nRun 'pigeon setup-whatsapp' or 'pigeon setup-slack' first", paths.ConfigPath())
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	msgHub := hub.New()
	apiServer := api.NewServer(msgHub)

	waMgr := daemon.NewWhatsAppManager(apiServer)
	go waMgr.Run(ctx, cfg.WhatsApp)

	slackMgr := daemon.NewSlackManager(apiServer)
	go slackMgr.Run(ctx, cfg.Slack)

	go apiServer.Start(ctx, paths.SocketPath())

	slog.Info("daemon started",
		"whatsapp_accounts", len(cfg.WhatsApp),
		"slack_workspaces", len(cfg.Slack))

	<-ctx.Done()
	slog.Info("shutting down")
	return nil
}
