// Package pctx resolves the "context" dimension of the read protocol.
//
// A context is a named set of accounts from config.yaml that forms a
// workspace boundary. Given a context and a source type, the resolver
// determines which account(s) to operate on. See docs/read-protocol.md.
package pctx

import "fmt"

// Source identifies a data type for the read protocol. It is always the
// first positional argument to "pigeon read".
type Source string

const (
	SourceGmail    Source = "gmail"
	SourceCalendar Source = "calendar"
	SourceDrive    Source = "drive"
	SourceSlack    Source = "slack"
	SourceWhatsApp Source = "whatsapp"
	SourceLinear   Source = "linear"
)

// AllSources lists every valid source in display order.
var AllSources = []Source{
	SourceGmail,
	SourceCalendar,
	SourceDrive,
	SourceSlack,
	SourceWhatsApp,
	SourceLinear,
}

// Platform is the storage-level platform that a source maps to. Platform
// names match the top-level directory names under the data root and the
// keys used in config.yaml contexts.
type Platform string

const (
	PlatformGWS      Platform = "gws"
	PlatformSlack    Platform = "slack"
	PlatformWhatsApp Platform = "whatsapp"
	PlatformLinear   Platform = "linear"
)

// sourcePlatform maps each source to its platform.
var sourcePlatform = map[Source]Platform{
	SourceGmail:    PlatformGWS,
	SourceCalendar: PlatformGWS,
	SourceDrive:    PlatformGWS,
	SourceSlack:    PlatformSlack,
	SourceWhatsApp: PlatformWhatsApp,
	SourceLinear:   PlatformLinear,
}

// ParseSource parses a string into a Source. Returns an error listing
// valid sources if the input is not recognized.
func ParseSource(s string) (Source, error) {
	src := Source(s)
	if _, ok := sourcePlatform[src]; ok {
		return src, nil
	}
	return "", fmt.Errorf("unknown source %q — valid sources: gmail, calendar, drive, slack, whatsapp, linear", s)
}

// Platform returns the storage platform for this source.
func (s Source) Platform() Platform {
	return sourcePlatform[s]
}

// ContextKey returns the config.Context map key for this source's platform.
// This is the key used to look up account identifiers in a context.
func (s Source) ContextKey() string {
	return string(s.Platform())
}
