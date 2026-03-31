package main

import "github.com/spf13/cobra"

// flagsToArgs converts cobra flags into the -key=value args format
// expected by the existing Run* functions in internal/commands.
func flagsToArgs(cmd *cobra.Command, names ...string) []string {
	var args []string
	for _, name := range names {
		f := cmd.Flags().Lookup(name)
		if f != nil && f.Changed {
			args = append(args, "-"+name+"="+f.Value.String())
		}
	}
	return args
}
