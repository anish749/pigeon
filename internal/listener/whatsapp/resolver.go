package whatsapp

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

func containsLower(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), substr)
}

// Resolver provides consistent name resolution for WhatsApp contacts and groups.
// It reads from whatsmeow's ContactStore (persisted in SQLite) and maintains an
// in-memory cache for group names. Both the real-time listener and history sync
// use this to produce identical directory names and sender attributions.
type Resolver struct {
	client   *whatsmeow.Client
	groups   map[types.JID]string
	groupsMu sync.RWMutex
}

// NewResolver creates a Resolver backed by the client's contact store.
func NewResolver(client *whatsmeow.Client) *Resolver {
	return &Resolver{
		client: client,
		groups: make(map[types.JID]string),
	}
}

// ContactName returns the best display name for a user JID.
// Priority: FullName (address book) → PushName → BusinessName → phone number.
func (r *Resolver) ContactName(ctx context.Context, jid types.JID) string {
	jid = r.ResolveJID(ctx, jid)

	info, err := r.client.Store.Contacts.GetContact(ctx, jid)
	if err == nil && info.Found {
		if info.FullName != "" {
			return info.FullName
		}
		if info.PushName != "" {
			return info.PushName
		}
		if info.BusinessName != "" {
			return info.BusinessName
		}
	}
	return "+" + jid.User
}

// GroupName returns the name for a group JID.
// Checks the local cache first, then fetches from the WhatsApp server.
func (r *Resolver) GroupName(ctx context.Context, jid types.JID) string {
	r.groupsMu.RLock()
	name, ok := r.groups[jid]
	r.groupsMu.RUnlock()
	if ok && name != "" {
		return name
	}

	info, err := r.client.GetGroupInfo(ctx, jid)
	if err != nil {
		slog.WarnContext(ctx, "whatsapp: failed to fetch group info", "jid", jid, "error", err)
		return jid.User
	}
	r.SetGroupName(jid, info.Name)
	return info.Name
}

// SetGroupName caches a group name.
func (r *Resolver) SetGroupName(jid types.JID, name string) {
	if name == "" {
		return
	}
	r.groupsMu.Lock()
	r.groups[jid] = name
	r.groupsMu.Unlock()
}

// ConvDir returns the conversation directory name for file storage.
// DMs produce "+phone" (phone number only); groups produce the sanitized group name.
func (r *Resolver) ConvDir(ctx context.Context, chatJID types.JID) string {
	if chatJID.Server == types.GroupServer {
		return SanitizeFilename(r.GroupName(ctx, chatJID))
	}

	jid := r.ResolveJID(ctx, chatJID)
	return "+" + jid.User
}

// ContactMatch represents a contact that matched a search query.
type ContactMatch struct {
	JID   types.JID
	Name  string
	Phone string // "+14155559876"
}

// FindJID searches the contact store for a JID matching the query string.
// Matches case-insensitively against FullName, PushName, BusinessName, and phone number.
// If exactly one match, returns it. If multiple, returns all matches via ErrAmbiguous.
func (r *Resolver) FindJID(ctx context.Context, query string) (types.JID, error) {
	contacts, err := r.client.Store.Contacts.GetAllContacts(ctx)
	if err != nil {
		return types.JID{}, fmt.Errorf("load contacts: %w", err)
	}
	q := strings.ToLower(query)

	var matches []ContactMatch

	for jid, info := range contacts {
		phone := "+" + jid.User
		if containsLower(info.FullName, q) ||
			containsLower(info.PushName, q) ||
			containsLower(info.BusinessName, q) ||
			containsLower(phone, q) {
			name := info.FullName
			if name == "" {
				name = info.PushName
			}
			if name == "" {
				name = info.BusinessName
			}
			matches = append(matches, ContactMatch{
				JID:   jid,
				Name:  name,
				Phone: phone,
			})
		}
	}

	if len(matches) == 0 {
		return types.JID{}, fmt.Errorf("no contact matching %q", query)
	}
	if len(matches) == 1 {
		return types.NewJID(matches[0].JID.User, types.DefaultUserServer), nil
	}

	return types.JID{}, &AmbiguousContactError{Query: query, Matches: matches}
}

// AmbiguousContactError is returned when a contact query matches multiple people.
// The caller should enrich Matches with conversation activity before displaying.
type AmbiguousContactError struct {
	Query   string
	Matches []ContactMatch
}

func (e *AmbiguousContactError) Error() string {
	return fmt.Sprintf("multiple contacts match %q (%d matches)", e.Query, len(e.Matches))
}

// ResolveJID converts a LID (hidden user) JID to a phone-number JID if possible.
func (r *Resolver) ResolveJID(ctx context.Context, jid types.JID) types.JID {
	if jid.Server == types.HiddenUserServer {
		pnJID, err := r.client.Store.LIDs.GetPNForLID(ctx, jid)
		if err == nil && !pnJID.IsEmpty() {
			return pnJID
		}
	}
	return jid
}
