package read

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"github.com/anish749/pigeon/internal/paths"
)

// GrepOpts controls content search behavior.
type GrepOpts struct {
	Query           string
	Since           time.Duration
	Context         int  // -C lines of context
	FilesOnly       bool // -l: return file paths only
	Count           bool // -c: return match counts per file
	CaseInsensitive bool // -i
	FixedStrings    bool // -F: literal match, no regex
	JSON            bool // --json: structured JSON output (for machine parsing)
}

// Grep runs a content search over data files under dir. Returns raw rg
// output.
//
// Files covered (same set as Glob):
//   - Messaging JSONL date and thread files.
//   - Gmail and Calendar JSONL date files.
//   - Drive content files (.md, .csv, comments.jsonl) whose sibling
//     drive-meta file falls within the since window.
//
// When Since is zero, all data files under dir are searched. When Since is
// set, the file list is pre-computed via Glob (to handle Drive content files
// which cannot be filtered by filename glob alone) and passed to rg as
// positional arguments.
func Grep(dir string, opts GrepOpts) ([]byte, error) {
	args := buildGrepFlags(opts)

	if opts.Since == 0 {
		// Without a time window, let rg walk the tree itself and filter by
		// extension glob. This is faster than pre-computing the file list.
		args = append(args, "--glob", "*"+paths.FileExt)
		for _, ext := range paths.DriveContentExts {
			if ext != paths.FileExt {
				args = append(args, "--glob", "*"+ext)
			}
		}
		args = append(args, opts.Query, dir)
		return runGrep(args)
	}

	// With a time window, pre-compute the file list via Glob. This handles
	// Drive content files (which aren't date-named) by resolving sibling
	// files of drive-meta matches.
	files, err := Glob(dir, opts.Since)
	if err != nil {
		return nil, fmt.Errorf("enumerate grep files: %w", err)
	}
	if len(files) == 0 {
		return nil, nil
	}

	args = append(args, opts.Query)
	args = append(args, files...)
	return runGrep(args)
}

// GrepMany runs Grep across multiple roots and concatenates the raw rg output.
// Exit-code-1 "no matches" remains a quiet success for each individual root.
func GrepMany(dirs []string, opts GrepOpts) ([]byte, error) {
	var (
		out  bytes.Buffer
		errs []error
	)
	for _, dir := range dirs {
		data, err := Grep(dir, opts)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out.Write(data)
	}
	if out.Len() == 0 {
		return nil, errorsJoin(errs)
	}
	return out.Bytes(), errorsJoin(errs)
}

// buildGrepFlags constructs the rg flag list for the given options, excluding
// the query and file/dir arguments.
func buildGrepFlags(opts GrepOpts) []string {
	var args []string
	if opts.JSON {
		args = append(args, "--json")
	} else {
		args = append(args, "--color=never")
	}
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
	return args
}

// runGrep executes rg and returns its output. Exit code 1 (no matches) is
// treated as success with no output.
func runGrep(args []string) ([]byte, error) {
	out, err := exec.Command("rg", args...).Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil, nil // no matches
		}
		return nil, fmt.Errorf("rg: %w", err)
	}
	return out, nil
}

func errorsJoin(errs []error) error {
	return errors.Join(errs...)
}
