package main

import (
	"os"

	"github.com/anish/claude-msg-utils/internal/cli"
)

var version = "dev"

func main() {
	if err := cli.Execute(version); err != nil {
		os.Exit(1)
	}
}
