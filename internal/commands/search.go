package commands

import (
	"fmt"
	"os"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/search"
	"github.com/anish749/pigeon/internal/timeutil"
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

	var sinceDur time.Duration
	if p.Since != "" {
		d, err := timeutil.ParseDuration(p.Since)
		if err != nil {
			return fmt.Errorf("invalid --since value %q: %w", p.Since, err)
		}
		sinceDur = d
	}

	output, err := search.Grep(p.Query, searchDir, sinceDur, p.Context)
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
