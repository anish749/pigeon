package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/anish749/pigeon/internal/paths"
)

type SearchParams struct {
	Query    string
	Platform string
	Account  string
	Since    string
	Context  int // lines of context around each match
}

func RunSearch(p SearchParams) error {
	searchDir := searchPath(p.Platform, p.Account)
	if _, err := os.Stat(searchDir); os.IsNotExist(err) {
		return fmt.Errorf("no data at %s", searchDir)
	}

	includes, err := dateFileIncludes(searchDir, p.Since)
	if err != nil {
		return err
	}

	return runGrep(p.Query, searchDir, includes, p.Context)
}

// searchPath returns the directory to search based on platform/account filters.
// No filters: search the entire data dir. Platform only: search that platform.
// Both: search that specific account.
func searchPath(platform, account string) string {
	base := paths.DataDir()
	if platform == "" {
		return base
	}
	if account == "" {
		return filepath.Join(base, platform)
	}
	return filepath.Join(base, platform, account)
}

// dateFileIncludes returns --include glob patterns to restrict search to date
// files within the --since window. If since is empty, returns nil (search all .txt files).
func dateFileIncludes(searchDir, since string) ([]string, error) {
	if since == "" {
		return []string{"*.txt"}, nil
	}

	dur, err := parseDuration(since)
	if err != nil {
		return nil, fmt.Errorf("invalid --since value %q: %w", since, err)
	}

	cutoff := time.Now().Add(-dur).Truncate(24 * time.Hour)
	var includes []string

	// Walk to find date files, filter by filename date.
	filepath.Walk(searchDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		name := info.Name()
		if len(name) != len("YYYY-MM-DD.txt") {
			return nil
		}
		dateStr := name[:10]
		t, parseErr := time.Parse("2006-01-02", dateStr)
		if parseErr != nil {
			return nil
		}
		if !t.Before(cutoff) {
			includes = append(includes, name)
		}
		return nil
	})

	if len(includes) == 0 {
		return nil, fmt.Errorf("no date files within --%s window", since)
	}
	return includes, nil
}

// runGrep executes rg (or falls back to grep) with the given query and options.
func runGrep(query, dir string, includes []string, context int) error {
	if rgPath, err := exec.LookPath("rg"); err == nil {
		return runRg(rgPath, query, dir, includes, context)
	}
	return runGrepFallback(query, dir, includes, context)
}

func runRg(rgPath, query, dir string, includes []string, context int) error {
	args := []string{"--color=auto"}
	for _, inc := range includes {
		args = append(args, "--glob", inc)
	}
	if context > 0 {
		args = append(args, fmt.Sprintf("-C%d", context))
	}
	args = append(args, query, dir)

	cmd := exec.Command(rgPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
		fmt.Println("No matches found.")
		return nil
	}
	return err
}

func runGrepFallback(query, dir string, includes []string, context int) error {
	args := []string{"-r", "--color=auto"}
	for _, inc := range includes {
		args = append(args, "--include", inc)
	}
	if context > 0 {
		args = append(args, fmt.Sprintf("-C%d", context))
	}
	args = append(args, query, dir)

	cmd := exec.Command("grep", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
		fmt.Println("No matches found.")
		return nil
	}
	return err
}
