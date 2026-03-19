package commands

import (
	"context"
	"log/slog"
	"strings"

	"go.mau.fi/whatsmeow/types"

	"github.com/anish/claude-msg-utils/internal/config"
	walistener "github.com/anish/claude-msg-utils/internal/listener/whatsapp"
	"github.com/anish/claude-msg-utils/internal/store"
)

// loadAliases returns contact name aliases for a given platform/account.
// For WhatsApp accounts, it reads from the whatsmeow contact store.
// Returns nil (no aliases) for other platforms or on error.
func loadAliases(platform, account string) map[string][]string {
	if platform != "whatsapp" {
		return nil
	}
	cfg, err := config.Load()
	if err != nil {
		return nil
	}
	for _, wa := range cfg.WhatsApp {
		if wa.Account == account {
			jid, err := types.ParseJID(wa.DeviceJID)
			if err != nil {
				slog.Warn("invalid device JID in config", "jid", wa.DeviceJID, "error", err)
				return nil
			}
			aliases, err := walistener.LoadContactAliases(context.Background(), wa.DB, jid)
			if err != nil {
				slog.Warn("failed to load WhatsApp contacts", "account", account, "error", err)
				return nil
			}
			return aliases
		}
	}
	return nil
}

// enrichSearchResults replaces phone senders with names and resolves
// conversation directory names to display names in search results.
func enrichSearchResults(results []store.SearchResult, platform, account string) {
	// Load aliases for each unique platform/account pair in the results.
	type key struct{ platform, account string }
	aliasCache := make(map[key]map[string][]string)

	if platform != "" && account != "" {
		aliasCache[key{platform, account}] = loadAliases(platform, account)
	}

	for i, r := range results {
		k := key{r.Platform, r.Account}
		aliases, ok := aliasCache[k]
		if !ok {
			aliases = loadAliases(r.Platform, r.Account)
			aliasCache[k] = aliases
		}

		// Enrich message lines in the section.
		results[i].Lines = enrichLines(r.Lines, aliases)

		// Resolve conversation dir to display name.
		if names, ok := aliases[r.Conversation]; ok && len(names) > 0 {
			results[i].Conversation = names[0]
		}
	}
}

// enrichLines replaces phone number senders in message lines with contact names.
// Message format: [2026-03-18 21:14:48] +19175305966: text
func enrichLines(lines []string, aliases map[string][]string) []string {
	if len(aliases) == 0 {
		return lines
	}
	for i, line := range lines {
		if len(line) < 23 || line[0] != '[' {
			continue
		}
		rest := line[22:]
		colonIdx := strings.Index(rest, ": ")
		if colonIdx < 0 {
			continue
		}
		sender := rest[:colonIdx]
		if names, ok := aliases[sender]; ok && len(names) > 0 {
			lines[i] = line[:22] + names[0] + line[22+colonIdx:]
		}
	}
	return lines
}
