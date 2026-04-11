// Package identity implements the Pigeon Identity Protocol: cross-source
// person identity storage and resolution. See docs/identity-protocol.md.
package identity

// Signal represents an identity observation from a listener or poller.
// Each signal carries whatever identifiers the source provides. At least
// one of Email, Slack, or Phone must be non-empty.
type Signal struct {
	Email string         // email address
	Name  string         // display name (best-effort)
	Slack *SlackIdentity // non-nil if signal came from Slack
	Phone string         // E.164 format, e.g. "+15551234567"
}

// SlackIdentity holds a Slack-specific identity within a workspace.
type SlackIdentity struct {
	Workspace string // workspace slug (e.g. "acme-corp")
	ID        string // Slack user ID (U-prefixed)
	Mention   string // display name as used after @ in stored messages
}
