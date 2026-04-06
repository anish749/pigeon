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
	// **/threads/*.txt matches nested paths like #general/threads/1742100000.txt.
	includes = append(includes, paths.ThreadGlobRg)

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
	// grep --include only matches basenames, so it can't handle the nested
	// thread path pattern. Split into date file includes (--include) and
	// thread files (find -exec grep).
	var dateIncludes []string
	searchThreads := false
	for _, inc := range includes {
		if inc == paths.ThreadGlobRg {
			searchThreads = true
		} else {
			dateIncludes = append(dateIncludes, inc)
		}
	}

	var out []byte

	// Run 1: date files via grep --include.
	if len(dateIncludes) > 0 {
		args := []string{"-r", "--color=never"}
		for _, inc := range dateIncludes {
			args = append(args, "--include", inc)
		}
		if context > 0 {
			args = append(args, fmt.Sprintf("-C%d", context))
		}
		args = append(args, query, dir)

		dateOut, err := exec.Command("grep", args...).Output()
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
				// No matches in date files, continue to threads.
			} else {
				return nil, err
			}
		}
		out = dateOut
	}

	// Run 2: thread files via find -exec grep.
	if searchThreads {
		threadOut, err := grepThreadFiles(query, dir, context)
		if err != nil {
			return out, err
		}
		out = append(out, threadOut...)
	}

	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// grepThreadFiles searches thread files using find(1) to locate them and
// grep to search. This is needed because grep --include only matches
// basenames and can't express a path pattern like */threads/*.txt.
func grepThreadFiles(query, dir string, context int) ([]byte, error) {
	grepArgs := []string{"-H", "--color=never"}
	if context > 0 {
		grepArgs = append(grepArgs, fmt.Sprintf("-C%d", context))
	}
	grepArgs = append(grepArgs, query)

	// find <dir> -path '*/threads/*.txt' -exec grep -H --color=never <query> {} +
	// The + terminator batches files into one grep call. If find matches
	// no files, grep is never invoked and find exits 0.
	args := []string{dir, "-path", paths.ThreadGlobFind, "-exec", "grep"}
	args = append(args, grepArgs...)
	args = append(args, "{}", "+")

	out, err := exec.Command("find", args...).Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil, nil // no matches
		}
		return nil, err
	}
	return out, nil
}
