package read

import (
	"fmt"
	"os/exec"
	"time"

	"github.com/anish749/pigeon/internal/paths"
)

// GrepOpts controls content search behavior.
type GrepOpts struct {
	Query          string
	Since          time.Duration
	Context        int  // -C lines of context
	FilesOnly      bool // -l: return file paths only
	Count          bool // -c: return match counts per file
	CaseInsensitive bool // -i
	FixedStrings   bool // -F: literal match, no regex
}

// Grep runs a content search over data files under dir. Returns raw rg
// output. When Since is set, date files are filtered by the same date
// globs as Glob, and thread files are included unfiltered (the caller
// should post-filter thread results by message timestamp if needed).
func Grep(dir string, opts GrepOpts) ([]byte, error) {
	args := []string{"--color=never"}

	if opts.FilesOnly {
		args = append(args, "-l")
	}
	if opts.Count {
		args = append(args, "-c")
	}
	if opts.CaseInsensitive {
		args = append(args, "-i")
	}
	if opts.FixedStrings {
		args = append(args, "-F")
	}
	if opts.Context > 0 {
		args = append(args, fmt.Sprintf("-C%d", opts.Context))
	}

	// File selection: same --since logic as Glob for date files.
	if opts.Since > 0 {
		for _, g := range dateGlobs(opts.Since) {
			args = append(args, "--glob", g)
		}
		// Include all thread files — post-filter by message ts is the
		// caller's responsibility (same as Glob uses rg -l for threads,
		// but grep needs content, not just paths).
		args = append(args, "--glob", paths.ThreadGlobRg)
	} else {
		args = append(args, "--glob", "*"+paths.FileExt)
	}

	args = append(args, opts.Query, dir)

	out, err := exec.Command("rg", args...).Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil, nil // no matches
		}
		return nil, fmt.Errorf("rg: %w", err)
	}
	return out, nil
}
