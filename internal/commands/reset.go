package commands

import (
	"fmt"
	"os"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/paths"
)

func RunReset(platform, acctName string) error {
	acct := account.New(platform, acctName)
	dir := paths.DefaultDataRoot().AccountFor(acct).Path()

	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("no data found for %s", acct.Display())
	}

	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("failed to delete %s: %w", dir, err)
	}

	fmt.Printf("Deleted all data for %s\n", acct.Display())
	return nil
}
