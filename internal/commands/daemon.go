package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/anish749/pigeon/internal/api"
	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/daemon"
	daemonclient "github.com/anish749/pigeon/internal/daemon/client"
	"github.com/anish749/pigeon/internal/hub"
	"github.com/anish749/pigeon/internal/logging"
	"github.com/anish749/pigeon/internal/outbox"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/selfupdate"
	"github.com/anish749/pigeon/internal/store"
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
	if !running {
		fmt.Println("Not running.")
		return nil
	}

	// Daemon is running — query its API for full status.
	resp, err := daemonclient.DefaultPgnHTTPClient.Get("http://pigeon/api/status")
	if err != nil {
		// Fall back to basic info if API is unreachable.
		fmt.Printf("Running (pid=%d, log=%s)\n", pid, paths.DaemonLogPath())
		return nil
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read status response: %w", err)
	}

	var status api.StatusResponse
	if err := json.Unmarshal(data, &status); err != nil {
		return fmt.Errorf("parse status response: %w", err)
	}

	uptime := time.Since(status.StartedAt).Truncate(time.Second)
	fmt.Printf("Running (pid=%d, version=%s, uptime=%s, log=%s)\n", status.PID, status.Version, uptime, status.LogFile)
	for platform, accounts := range status.Listeners {
		if len(accounts) > 0 {
			fmt.Printf("  %s: %s\n", platform, strings.Join(accounts, ", "))
		}
	}
	if len(status.ConnectedClaudeSessions) > 0 {
		fmt.Println("  connected claude sessions:")
		for _, s := range status.ConnectedClaudeSessions {
			fmt.Printf("    %s  %s  cwd=%s\n", s.Account, s.SessionID, s.CWD)
		}
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
func DaemonRun(version string) error {
	logging.InitFile(logging.Daemon)

	if err := daemon.WritePID(); err != nil {
		return fmt.Errorf("write PID file: %w", err)
	}
	defer daemon.RemovePID()

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if len(cfg.WhatsApp) == 0 && len(cfg.Slack) == 0 && len(cfg.GWS) == 0 {
		return fmt.Errorf("no listeners configured in %s\nRun 'pigeon setup-whatsapp', 'pigeon setup-slack', or add a gws account first", paths.ConfigPath())
	}

	// Check for updates before starting listeners. If an update is available,
	// re-exec immediately so listeners start with the new binary.
	if updated, err := selfupdate.CheckOnce(version); err != nil {
		slog.Error("startup update check failed", "error", err)
	} else if updated {
		return daemonReexec()
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	store := store.NewFSStore(paths.DefaultDataRoot())

	msgHub, err := hub.New(ctx, store)
	if err != nil {
		return fmt.Errorf("start hub: %w", err)
	}
	defer msgHub.Stop()

	ob := outbox.New()
	apiServer := api.NewServer(msgHub, ob, store, version)

	waMgr := daemon.NewWhatsAppManager(apiServer, store, msgHub.Route)
	go waMgr.Run(ctx, cfg.WhatsApp)

	slackMgr := daemon.NewSlackManager(apiServer, store, msgHub.Route)
	go slackMgr.Run(ctx, cfg.Slack)

	gwsMgr := daemon.NewGWSManager()
	go gwsMgr.Run(ctx, cfg.GWS)

	go apiServer.Start(ctx, paths.SocketPath())

	// Periodic update check — re-execs the daemon when a new version is installed.
	reexec := make(chan struct{}, 1)
	go selfupdate.DaemonAutoUpdate(ctx, version, func() {
		select {
		case reexec <- struct{}{}:
		default:
		}
	})

	slog.Info("daemon started",
		"version", version,
		"whatsapp_accounts", len(cfg.WhatsApp),
		"slack_workspaces", len(cfg.Slack),
		"gws_accounts", len(cfg.GWS))

	select {
	case <-ctx.Done():
		slog.Info("shutting down")
		return nil
	case <-reexec:
		cancel()
		return daemonReexec()
	}
}

// daemonReexec replaces the current process with the updated binary on disk.
// The new process starts from main() with the same PID and arguments.
func daemonReexec() error {
	slog.Info("update applied, re-execing daemon")

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate executable for re-exec: %w", err)
	}
	// Remove socket so the new process can bind it.
	if err := os.Remove(paths.SocketPath()); err != nil && !os.IsNotExist(err) {
		slog.Error("failed to remove socket before re-exec", "error", err)
	}
	return syscall.Exec(exePath, os.Args, os.Environ())
}
