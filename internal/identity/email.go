package identity

import "strings"

// gmailDomains is the set of domains where dot-insensitivity and plus
// addressing apply. Google Workspace custom domains are NOT included
// because dot and plus behavior varies by admin configuration.
var gmailDomains = map[string]bool{
	"gmail.com":     true,
	"googlemail.com": true,
}

// normalizeEmail returns a canonical form of an email address for identity
// matching. The result is always lowercase. For Gmail addresses, dots in
// the local part are removed and plus-addressed suffixes are stripped.
func normalizeEmail(email string) string {
	local, domain, ok := strings.Cut(email, "@")
	if !ok {
		return strings.ToLower(email)
	}

	domain = strings.ToLower(domain)
	local = strings.ToLower(local)

	if gmailDomains[domain] {
		// Strip plus-addressed suffix: alice+tag → alice
		if plus := strings.IndexByte(local, '+'); plus >= 0 {
			local = local[:plus]
		}
		// Remove dots: a.li.ce → alice
		local = strings.ReplaceAll(local, ".", "")
	}

	return local + "@" + domain
}
