package pctx

import (
	"fmt"
	"strings"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/config"
)

// ResolveOpts controls context resolution.
type ResolveOpts struct {
	// Context is the already-resolved context name. Computed by
	// ResolveContextName at the CLI boundary — the resolver never
	// reads env vars or config defaults itself.
	Context ContextName

	// Account bypasses context entirely and selects a specific account.
	// Corresponds to the -a flag.
	Account string
}

// Resolved holds the result of context resolution: the accounts to
// operate on for a given source.
type Resolved struct {
	// ContextName is the name of the active context, or empty if none.
	ContextName ContextName

	// Accounts is the resolved set of accounts for the requested source.
	// Always at least one element when error is nil.
	Accounts []account.Account
}

// Resolve determines which accounts to use for a given source.
//
// Resolution:
//  1. If opts.Account is set, bypass context and match directly.
//  2. If opts.Context is non-empty, look up accounts for the source's
//     platform within that context.
//  3. If no context, infer if only one account exists for the platform,
//     otherwise error.
func Resolve(cfg *config.Config, src Source, opts ResolveOpts) (*Resolved, error) {
	platform := src.Platform()

	// Direct account override (-a flag) bypasses context entirely.
	if opts.Account != "" {
		acct, err := matchAccount(cfg, platform, opts.Account)
		if err != nil {
			return nil, err
		}
		return &Resolved{Accounts: []account.Account{acct}}, nil
	}

	if opts.Context != "" {
		return resolveWithContext(cfg, src, platform, opts.Context)
	}
	return resolveWithoutContext(cfg, src, platform)
}

// resolveWithContext looks up the named context and finds accounts for the
// source's platform.
func resolveWithContext(cfg *config.Config, src Source, platform Platform, ctxName ContextName) (*Resolved, error) {
	ctx, ok := cfg.Contexts[string(ctxName)]
	if !ok {
		return nil, fmt.Errorf("unknown context %q — check contexts in config.yaml", ctxName)
	}

	identifiers := ctx.Accounts(string(platform))
	if len(identifiers) == 0 {
		return nil, fmt.Errorf("no %s account in context %q — cannot read %s", platform, ctxName, src)
	}

	var accounts []account.Account
	for _, id := range identifiers {
		acct, err := matchAccount(cfg, platform, id)
		if err != nil {
			return nil, fmt.Errorf("context %q references %s account %q: %w", ctxName, platform, id, err)
		}
		accounts = append(accounts, acct)
	}

	return &Resolved{
		ContextName: ctxName,
		Accounts:    accounts,
	}, nil
}

// resolveWithoutContext infers the account when no context is active.
// If exactly one account exists for the platform, it is used. Otherwise,
// an error asks the user to specify.
func resolveWithoutContext(cfg *config.Config, src Source, platform Platform) (*Resolved, error) {
	all := allAccountsForPlatform(cfg, platform)
	switch len(all) {
	case 0:
		return nil, fmt.Errorf("no %s accounts configured — cannot read %s", platform, src)
	case 1:
		return &Resolved{Accounts: all}, nil
	default:
		var names []string
		for _, a := range all {
			names = append(names, a.Name)
		}
		return nil, fmt.Errorf(
			"%d %s accounts configured (%s)\nspecify one with -a or set a context",
			len(all), platform, strings.Join(names, ", "),
		)
	}
}

// matchAccount finds a configured account matching the given identifier
// within the specified platform.
func matchAccount(cfg *config.Config, platform Platform, identifier string) (account.Account, error) {
	switch platform {
	case PlatformGWS:
		for _, g := range cfg.GWS {
			if g.Email == identifier {
				return account.New("gws", g.Email), nil
			}
		}
		return account.Account{}, fmt.Errorf("no GWS account with email %q in config", identifier)

	case PlatformSlack:
		for _, s := range cfg.Slack {
			if s.Workspace == identifier {
				return account.New("slack", s.Workspace), nil
			}
		}
		return account.Account{}, fmt.Errorf("no Slack workspace %q in config", identifier)

	case PlatformWhatsApp:
		for _, w := range cfg.WhatsApp {
			// Match by display name or phone number embedded in DeviceJID.
			if w.Account == identifier || matchWhatsAppPhone(w.DeviceJID, identifier) {
				return account.New("whatsapp", w.Account), nil
			}
		}
		return account.Account{}, fmt.Errorf("no WhatsApp account matching %q in config", identifier)

	case PlatformLinear:
		for _, l := range cfg.Linear {
			if l.Workspace == identifier || l.Account == identifier {
				return account.New("linear", l.Workspace), nil
			}
		}
		return account.Account{}, fmt.Errorf("no Linear workspace %q in config", identifier)

	default:
		return account.Account{}, fmt.Errorf("unknown platform %q", platform)
	}
}

// matchWhatsAppPhone checks if a DeviceJID contains the given phone number.
// DeviceJID format: "15551234567:0@s.whatsapp.net". Phone identifier may
// be "+15551234567" or "15551234567".
func matchWhatsAppPhone(deviceJID, phone string) bool {
	// Strip leading + from phone for comparison.
	phone = strings.TrimPrefix(phone, "+")
	if phone == "" {
		return false
	}
	// Extract digits before the first ':' or '@' in the JID.
	jidPhone := deviceJID
	if i := strings.IndexAny(jidPhone, ":@"); i > 0 {
		jidPhone = jidPhone[:i]
	}
	return jidPhone == phone
}

// allAccountsForPlatform returns every configured account for a platform.
func allAccountsForPlatform(cfg *config.Config, platform Platform) []account.Account {
	switch platform {
	case PlatformGWS:
		accounts := make([]account.Account, len(cfg.GWS))
		for i, g := range cfg.GWS {
			accounts[i] = account.New("gws", g.Email)
		}
		return accounts
	case PlatformSlack:
		accounts := make([]account.Account, len(cfg.Slack))
		for i, s := range cfg.Slack {
			accounts[i] = account.New("slack", s.Workspace)
		}
		return accounts
	case PlatformWhatsApp:
		accounts := make([]account.Account, len(cfg.WhatsApp))
		for i, w := range cfg.WhatsApp {
			accounts[i] = account.New("whatsapp", w.Account)
		}
		return accounts
	case PlatformLinear:
		accounts := make([]account.Account, len(cfg.Linear))
		for i, l := range cfg.Linear {
			accounts[i] = account.New("linear", l.Workspace)
		}
		return accounts
	default:
		return nil
	}
}
