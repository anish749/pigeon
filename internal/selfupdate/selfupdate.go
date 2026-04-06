package selfupdate

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	selfupdate "github.com/creativeprojects/go-selfupdate"

	"github.com/anish749/pigeon/internal/paths"
)

const repo = "anish749/pigeon"
const checkInterval = 24 * time.Hour

// Update checks for the latest release and replaces the binary if a newer version exists.
func Update(currentVersion string) (bool, error) {
	return doUpdate(currentVersion, true)
}

// AutoCheck checks for updates if 24 hours have passed since the last check.
// Runs silently in the background so it never blocks the user's command.
func AutoCheck(currentVersion string) {
	if currentVersion == "dev" {
		return
	}

	lastCheck, err := readLastCheck()
	if err != nil && !os.IsNotExist(err) {
		slog.Error("read last update check", "error", err)
	}
	if time.Since(lastCheck) < checkInterval {
		return
	}

	if err := writeLastCheck(); err != nil {
		slog.Error("write last update check", "error", err)
	}

	go func() {
		if _, err := doUpdate(currentVersion, false); err != nil {
			slog.Error("auto-update check failed", "error", err)
		}
	}()
}

// CheckOnce checks for an update and applies it if available. Returns true
// if the binary was replaced. Intended for use at daemon startup before
// listeners are started — avoids wasting work that a re-exec would redo.
func CheckOnce(currentVersion string) (bool, error) {
	if currentVersion == "dev" {
		return false, nil
	}
	return doUpdate(currentVersion, false)
}

const (
	// pollInterval is how often we check the wall clock for sleep/wake gaps.
	pollInterval = 1 * time.Minute
	// wakeThreshold is the minimum gap between ticks that indicates a sleep/wake cycle.
	// If a 1-minute ticker fires and more than 2 minutes have elapsed, we slept.
	wakeThreshold = 2 * time.Minute
)

// DaemonAutoUpdate monitors for two events that should trigger a re-exec:
//  1. Sleep/wake detected (wall clock jumped) — re-exec so the new process
//     runs CheckOnce before listeners reconnect.
//  2. Periodic update check (every checkInterval) — re-exec if a new version
//     was downloaded.
//
// onReexec is called when the daemon should re-exec itself.
func DaemonAutoUpdate(ctx context.Context, currentVersion string, onReexec func()) {
	if currentVersion == "dev" {
		return
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	lastTick := time.Now()
	lastUpdateCheck := time.Now() // startup CheckOnce already ran

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			elapsed := now.Sub(lastTick)
			lastTick = now

			// Detect sleep/wake: if significantly more time passed than expected,
			// re-exec so the new process runs CheckOnce before listeners reconnect.
			if elapsed > wakeThreshold {
				slog.Info("sleep/wake detected, re-execing to run update check",
					"gap", elapsed.Round(time.Second))
				onReexec()
				return
			}

			// Periodic update check.
			if time.Since(lastUpdateCheck) >= checkInterval {
				lastUpdateCheck = now
				updated, err := doUpdate(currentVersion, false)
				if err != nil {
					slog.Error("daemon auto-update check failed", "error", err)
					continue
				}
				if updated {
					onReexec()
					return
				}
			}
		}
	}
}

func doUpdate(currentVersion string, verbose bool) (bool, error) {
	if currentVersion == "dev" {
		slog.Warn("skipping update check: running dev build")
		return false, nil
	}

	source, err := selfupdate.NewGitHubSource(selfupdate.GitHubConfig{})
	if err != nil {
		return false, fmt.Errorf("create update source: %w", err)
	}

	updater, err := selfupdate.NewUpdater(selfupdate.Config{
		Source:    source,
		Validator: &selfupdate.ChecksumValidator{UniqueFilename: "checksums.txt"},
	})
	if err != nil {
		return false, fmt.Errorf("create updater: %w", err)
	}

	latest, found, err := updater.DetectLatest(context.Background(), selfupdate.ParseSlug(repo))
	if err != nil {
		return false, fmt.Errorf("check for updates: %w", err)
	}
	if !found {
		if verbose {
			return false, fmt.Errorf("no releases found for %s", repo)
		}
		return false, nil
	}

	if latest.LessOrEqual(currentVersion) {
		if verbose {
			fmt.Fprintf(os.Stderr, "Already up to date (v%s)\n", currentVersion)
		}
		return false, nil
	}

	fmt.Fprintf(os.Stderr, "Updating pigeon v%s → v%s...\n", currentVersion, latest.Version())

	exePath, err := selfupdate.ExecutablePath()
	if err != nil {
		return false, fmt.Errorf("locate executable: %w", err)
	}

	if err := updater.UpdateTo(context.Background(), latest, exePath); err != nil {
		return false, fmt.Errorf("update failed: %w", err)
	}

	fmt.Fprintf(os.Stderr, "✓ Updated to v%s\n", latest.Version())
	return true, nil
}

func readLastCheck() (time.Time, error) {
	data, err := os.ReadFile(paths.LastUpdateCheckPath())
	if err != nil {
		return time.Time{}, err
	}
	ts, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(ts, 0), nil
}

func writeLastCheck() error {
	path := paths.LastUpdateCheckPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	return os.WriteFile(path, []byte(strconv.FormatInt(time.Now().Unix(), 10)), 0600)
}
