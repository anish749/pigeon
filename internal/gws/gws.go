// Package gws shells out to the gws CLI for Google Workspace API calls.
package gws

import (
	"encoding/json"
	"fmt"
	"os/exec"
)

// Run executes a gws CLI command and returns the raw JSON output.
func Run(args ...string) ([]byte, error) {
	cmd := exec.Command("gws", args...)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("gws %v: %s", args, exitErr.Stderr)
		}
		return nil, fmt.Errorf("gws %v: %w", args, err)
	}
	return out, nil
}

// RunParsed executes a gws CLI command and unmarshals the JSON output into dst.
func RunParsed(dst any, args ...string) error {
	out, err := Run(args...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(out, dst); err != nil {
		return fmt.Errorf("parse gws output: %w", err)
	}
	return nil
}

// ParamsJSON marshals a map to a JSON string for --params flags.
func ParamsJSON(m map[string]string) string {
	b, _ := json.Marshal(m)
	return string(b)
}
