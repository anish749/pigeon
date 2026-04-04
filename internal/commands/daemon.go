package commands

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/anish/claude-msg-utils/internal/api"
	"github.com/anish/claude-msg-utils/internal/claude"
	"github.com/anish/claude-msg-utils/internal/config"
	"github.com/anish/claude-msg-utils/internal/daemon"
	"github.com/anish/claude-msg-utils/internal/hub"
)

// claudeSessionStore adapts the claude package to the hub.SessionStore interface.
type claudeSessionStore struct{}

func (claudeSessionStore) ListSessions() ([]hub.SessionInfo, error) {
	sessions, err := claude.ListSessions()
	if err != nil {
		return nil, err
	}
	var infos []hub.SessionInfo
	for _, s := range sessions {
		infos = append(infos, hub.SessionInfo{
			Platform:      s.Platform,
			Account:       s.Account,
			SessionID:     s.SessionID,
			CWD:           s.CWD,
			LastDelivered: s.LastDelivered,
		})
	}
	return infos, nil
}

func (claudeSessionStore) FindByID(sessionID string) (*hub.SessionInfo, error) {
	s, err := claude.FindSessionByID(sessionID)
	if err != nil {
		return nil, err
	}
	if s == nil {
		return nil, nil
	}
	return &hub.SessionInfo{
		Platform:      s.Platform,
		Account:       s.Account,
		SessionID:     s.SessionID,
		CWD:           s.CWD,
		LastDelivered: s.LastDelivered,
	}, nil
}

func (claudeSessionStore) UpdateLastDelivered(platform, account string, t time.Time) error {
	return claude.UpdateLastDelivered(platform, account, t)
}

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
		fmt.Printf("Running (pid=%d, log=%s)\n", pid, daemon.LogPath())
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
		Filename:   daemon.LogPath(),
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
		return fmt.Errorf("no listeners configured in %s\nRun 'pigeon setup-whatsapp' or 'pigeon setup-slack' first", config.ConfigPath())
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	msgHub := hub.New(ctx, claudeSessionStore{}, hub.StoreMessageReader{})
	defer msgHub.Stop()
	apiServer := api.NewServer(msgHub)

	waMgr := daemon.NewWhatsAppManager(apiServer, msgHub.Route)
	go waMgr.Run(ctx, cfg.WhatsApp)

	slackMgr := daemon.NewSlackManager(apiServer, msgHub.Route)
	go slackMgr.Run(ctx, cfg.Slack)

	go apiServer.Start(ctx, daemon.SocketPath())

	slog.Info("daemon started",
		"whatsapp_accounts", len(cfg.WhatsApp),
		"slack_workspaces", len(cfg.Slack))

	<-ctx.Done()
	slog.Info("shutting down")
	return nil
}
