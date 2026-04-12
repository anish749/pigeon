package daemon

import (
	"context"
	"fmt"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	_ "modernc.org/sqlite"

	"github.com/anish749/pigeon/internal/walog"
)

// ConnectWhatsApp creates a whatsmeow client for a known device, without
// calling Connect(). Shared between the runtime listener and one-shot
// commands (e.g. pigeon reset-whatsapp).
func ConnectWhatsApp(ctx context.Context, dbPath string, jid types.JID) (*whatsmeow.Client, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)", dbPath)
	container, err := sqlstore.New(ctx, "sqlite", dsn, walog.New(ctx, "whatsapp-db"))
	if err != nil {
		return nil, fmt.Errorf("create device store: %w", err)
	}

	device, err := container.GetDevice(ctx, jid)
	if err != nil {
		return nil, fmt.Errorf("get device for JID %s: %w", jid.String(), err)
	}
	if device == nil {
		return nil, fmt.Errorf("no device found for JID %s — run setup-whatsapp first", jid.String())
	}

	return whatsmeow.NewClient(device, walog.New(ctx, "whatsapp")), nil
}
