package commands

import (
	"fmt"
	"os"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/paths"
)

func RunReset(platform, acctName string) error {
	acct := account.New(platform, acctName)
	acctDir := paths.DefaultDataRoot().AccountFor(acct)
	dir := acctDir.Path()

	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("no data found for %s", acct.Display())
	}

	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("failed to delete %s: %w", dir, err)
	}

	fmt.Printf("Deleted all data for %s\n", acct.Display())

	// For WhatsApp, leave a marker so the daemon requests a full history
	// re-sync on next connect. Without this, WhatsApp won't send history
	// again because the device is already paired.
	if platform == "whatsapp" {
		markerPath := acctDir.ResyncMarkerPath()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create account dir for resync marker: %w", err)
		}
		if err := os.WriteFile(markerPath, nil, 0o644); err != nil {
			return fmt.Errorf("write resync marker: %w", err)
		}
		fmt.Println("History will re-sync on next daemon start.")
	}

	return nil
}
