package whatsapp

import (
	"context"
	"fmt"

	_ "modernc.org/sqlite"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
)

// LoadContactAliases opens a whatsmeow database and returns a map from phone
// directory name (e.g. "+14155559876") to searchable name variants.
// The first entry in each slice is the best display name.
// Used by search/list/read commands for name resolution without a running daemon.
func LoadContactAliases(ctx context.Context, dbPath string, deviceJID types.JID) (map[string][]string, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)", dbPath)
	container, err := sqlstore.New(ctx, "sqlite", dsn, nil)
	if err != nil {
		return nil, fmt.Errorf("open whatsmeow store: %w", err)
	}

	device, err := container.GetDevice(ctx, deviceJID)
	if err != nil {
		return nil, fmt.Errorf("get device %s: %w", deviceJID, err)
	}
	if device == nil {
		return nil, fmt.Errorf("no device found for %s", deviceJID)
	}

	contacts, err := device.Contacts.GetAllContacts(ctx)
	if err != nil {
		return nil, fmt.Errorf("get contacts: %w", err)
	}

	aliases := make(map[string][]string, len(contacts))
	for jid, info := range contacts {
		if jid.Server != types.DefaultUserServer {
			continue
		}
		phone := "+" + jid.User

		// Order matters: first entry becomes the display name.
		var names []string
		if info.FullName != "" {
			names = append(names, info.FullName)
		}
		if info.PushName != "" {
			names = append(names, info.PushName)
		}
		if info.BusinessName != "" {
			names = append(names, info.BusinessName)
		}
		if len(names) > 0 {
			aliases[phone] = names
		}
	}
	return aliases, nil
}
