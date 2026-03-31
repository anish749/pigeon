package main

import (
	"os"

	"github.com/anish/claude-msg-utils/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
