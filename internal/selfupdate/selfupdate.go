package selfupdate

import (
	"context"
	"fmt"
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

	lastCheck, _ := readLastCheck()
	if time.Since(lastCheck) < checkInterval {
		return
	}

	writeLastCheck()

	go func() {
		doUpdate(currentVersion, false)
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

func lastCheckPath() string {
	return filepath.Join(paths.StateDir(), "last_update_check")
}

func readLastCheck() (time.Time, error) {
	data, err := os.ReadFile(lastCheckPath())
	if err != nil {
		return time.Time{}, err
	}
	ts, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(ts, 0), nil
}

func writeLastCheck() {
	path := lastCheckPath()
	os.MkdirAll(filepath.Dir(path), 0700)
	os.WriteFile(path, []byte(strconv.FormatInt(time.Now().Unix(), 10)), 0600)
}
