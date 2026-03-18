package commands

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/anish/claude-msg-utils/internal/store"
)

func RunReset(args []string) error {
	fs := flag.NewFlagSet("reset", flag.ContinueOnError)
	platform := fs.String("platform", "", "Platform name (e.g. slack, whatsapp) [required]")
	account := fs.String("account", "", "Account/workspace name [required]")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *platform == "" || *account == "" {
		msg := "usage: pigeon reset -platform=<platform> -account=<account>\n\nAvailable data:\n"
		platforms, err := store.ListPlatforms()
		if err != nil || len(platforms) == 0 {
			msg += "  (no data found)\n"
			return fmt.Errorf("%s", msg)
		}
		for _, p := range platforms {
			accounts, _ := store.ListAccounts(p)
			for _, a := range accounts {
				msg += fmt.Sprintf("  -platform=%s -account='%s'\n", p, a)
			}
		}
		return fmt.Errorf("%s", msg)
	}

	dir := filepath.Join(store.DataDir(), *platform, *account)

	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("no data found for %s/%s", *platform, *account)
	}

	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("failed to delete %s: %w", dir, err)
	}

	fmt.Printf("Deleted all data for %s/%s\n", *platform, *account)
	return nil
}
