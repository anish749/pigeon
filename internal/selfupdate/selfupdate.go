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

	"github.com/anish/claude-msg-utils/internal/paths"
)

const repo = "anish749/pigeon"
const checkInterval = 24 * time.Hour

// Update checks for the latest release and replaces the binary if a newer version exists.
func Update(currentVersion string) error {
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
		if err := doUpdate(currentVersion, false); err != nil {
			slog.Error("auto-update check failed", "error", err)
		}
	}()
}

func doUpdate(currentVersion string, verbose bool) error {
	source, err := selfupdate.NewGitHubSource(selfupdate.GitHubConfig{})
	if err != nil {
		return fmt.Errorf("create update source: %w", err)
	}

	updater, err := selfupdate.NewUpdater(selfupdate.Config{
		Source:    source,
		Validator: &selfupdate.ChecksumValidator{UniqueFilename: "checksums.txt"},
	})
	if err != nil {
		return fmt.Errorf("create updater: %w", err)
	}

	latest, found, err := updater.DetectLatest(context.Background(), selfupdate.ParseSlug(repo))
	if err != nil {
		return fmt.Errorf("check for updates: %w", err)
	}
	if !found {
		if verbose {
			return fmt.Errorf("no releases found for %s", repo)
		}
		return nil
	}

	if latest.LessOrEqual(currentVersion) {
		if verbose {
			fmt.Fprintf(os.Stderr, "Already up to date (v%s)\n", currentVersion)
		}
		return nil
	}

	fmt.Fprintf(os.Stderr, "Updating pigeon v%s → v%s...\n", currentVersion, latest.Version())

	exePath, err := selfupdate.ExecutablePath()
	if err != nil {
		return fmt.Errorf("locate executable: %w", err)
	}

	if err := updater.UpdateTo(context.Background(), latest, exePath); err != nil {
		return fmt.Errorf("update failed: %w", err)
	}

	fmt.Fprintf(os.Stderr, "✓ Updated to v%s\n", latest.Version())
	return nil
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
