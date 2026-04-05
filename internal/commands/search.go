package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/search"
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

	includes, err := fileIncludes(searchDir, p.Since)
	if err != nil {
		return err
	}

	var sinceDur time.Duration
	if p.Since != "" {
		d, err := parseDuration(p.Since)
		if err != nil {
			return fmt.Errorf("invalid --since value %q: %w", p.Since, err)
		}
		sinceDur = d
	}

	output, err := captureGrep(p.Query, searchDir, includes, p.Context)
	if err != nil {
		return err
	}
	if len(output) == 0 {
		fmt.Println("No matches found.")
		return nil
	}

	matches, parseErr := search.ParseGrepOutput(output, searchDir)
	if parseErr != nil {
		fmt.Fprintf(os.Stderr, "warning: some lines failed to parse: %v\n", parseErr)
	}

	if sinceDur > 0 {
		matches = search.FilterThreadsBySince(matches, sinceDur)
	}

	if len(matches) == 0 {
		fmt.Println("No matches found.")
		return nil
	}

	fmt.Printf("%d match(es) found:\n\n", len(matches))
	search.PrintSummary(matches, sinceDur)
	search.PrintGroupedResults(matches)

	return nil
}

// searchPath returns the directory to search based on platform/account filters.
func searchPath(platform, acctName string) string {
	root := paths.DefaultDataRoot()
	switch {
	case platform != "" && acctName != "":
		return root.AccountFor(account.New(platform, acctName)).Path()
	case platform != "":
		return root.Platform(platform).Path()
	default:
		return root.Path()
	}
}

// fileIncludes returns --glob patterns to restrict search to date files
// within the --since window plus all thread files. Thread files are always
// included because their filenames are timestamps, not dates — we can't
// filter them by name. If since is empty, returns *.txt (all files).
func fileIncludes(searchDir, since string) ([]string, error) {
	if since == "" {
		return []string{"*.txt"}, nil
	}

	dur, err := parseDuration(since)
	if err != nil {
		return nil, fmt.Errorf("invalid --since value %q: %w", since, err)
	}

	cutoff := time.Now().Add(-dur).Truncate(24 * time.Hour)
	seen := make(map[string]bool)

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
			seen[name] = true
		}
		return nil
	})

	var includes []string
	for name := range seen {
		includes = append(includes, name)
	}
	// Always include thread files — can't date-filter by filename.
	includes = append(includes, "threads/*.txt")

	if len(includes) == 1 {
		// Only the threads glob, no date files matched.
		return nil, fmt.Errorf("no date files within --%s window", since)
	}
	return includes, nil
}

// captureGrep runs rg (or grep) and returns the raw output.
func captureGrep(query, dir string, includes []string, context int) ([]byte, error) {
	if rgPath, err := exec.LookPath("rg"); err == nil {
		return captureRg(rgPath, query, dir, includes, context)
	}
	return captureGrepFallback(query, dir, includes, context)
}

func captureRg(rgPath, query, dir string, includes []string, context int) ([]byte, error) {
	args := []string{"--color=never"}
	for _, inc := range includes {
		args = append(args, "--glob", inc)
	}
	if context > 0 {
		args = append(args, fmt.Sprintf("-C%d", context))
	}
	args = append(args, query, dir)

	out, err := exec.Command(rgPath, args...).Output()
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
		return nil, nil
	}
	return out, err
}

func captureGrepFallback(query, dir string, includes []string, context int) ([]byte, error) {
	args := []string{"-r", "--no-color"}
	for _, inc := range includes {
		args = append(args, "--include", inc)
	}
	if context > 0 {
		args = append(args, fmt.Sprintf("-C%d", context))
	}
	args = append(args, query, dir)

	out, err := exec.Command("grep", args...).Output()
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
		return nil, nil
	}
	return out, err
}
