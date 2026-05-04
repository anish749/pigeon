package gmail

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/jhillyerd/enmime"

	"github.com/anish749/pigeon/internal/store/modelv1"
)

// mimeParser parses raw messages with malformed-part tolerance enabled.
// Real-world mail from buggy senders (e.g. bulk mailers whose template
// scripts paste shell error output into Content-Type headers) produces
// MIME parts that fail strict parsing. Without this option a single bad
// sub-part would drop the entire envelope — including the valid body
// text and headers we actually care about.
var mimeParser = enmime.NewParser(enmime.SkipMalformedParts(true))

// parseRawMessage decodes a base64url-encoded RFC 2822 message and
// extracts the body text, headers, and attachment metadata using enmime.
//
// The Gmail API returns the `raw` field as base64url with `=` padding.
// RFC 4648 allows padding to be omitted, and Go's encoding/base64 splits
// these into two strict variants (URLEncoding requires padding,
// RawURLEncoding rejects it), so we strip padding and use RawURLEncoding
// to accept either form.
func parseRawMessage(raw string) (*parsedMessage, error) {
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(raw, "="))
	if err != nil {
		return nil, fmt.Errorf("decode raw message: %w", err)
	}

	env, err := mimeParser.ReadEnvelope(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("parse MIME envelope: %w", err)
	}

	fromName, fromAddr := parseAddress(env.GetHeader("From"))
	to := parseAddresses(env.GetHeaderValues("To"))
	cc := parseAddresses(env.GetHeaderValues("Cc"))

	var attachments []modelv1.EmailAttachment
	for _, a := range env.Attachments {
		attachments = append(attachments, modelv1.EmailAttachment{
			ID:   a.ContentID,
			Type: a.ContentType,
			Name: a.FileName,
		})
	}

	// Collect severe parse errors so the caller can log them with
	// message-level context (ID, subject). SkipMalformedParts recovers
	// the envelope but records each dropped part here — without
	// surfacing them, parts would silently vanish.
	var warnings []string
	for _, e := range env.Errors {
		if e.Severe {
			warnings = append(warnings, fmt.Sprintf("%s: %s", e.Name, e.Detail))
		}
	}

	// env.Text is always populated — either from the text/plain part
	// or from enmime's HTML→text conversion. env.HTML is only populated
	// when a multipart message has an explicit text/html part.
	//
	// For single-part text/html messages, enmime populates env.Text
	// (via HTML→text conversion) but leaves env.HTML empty. Fall back
	// to env.Root.Content so we don't lose the original HTML.
	//
	// We store text always (greppable). We store html when present so
	// the protocol carries enough info to render rich content later.
	html := env.HTML
	if html == "" && env.Root != nil && env.Root.ContentType == "text/html" {
		html = string(env.Root.Content)
	}

	return &parsedMessage{
		subject:     env.GetHeader("Subject"),
		fromName:    fromName,
		fromAddr:    fromAddr,
		to:          to,
		cc:          cc,
		text:        env.Text,
		html:        html,
		attachments: attachments,
		warnings:    warnings,
	}, nil
}

type parsedMessage struct {
	subject     string
	fromName    string
	fromAddr    string
	to          []string
	cc          []string
	text        string
	html        string
	attachments []modelv1.EmailAttachment
	warnings    []string
}

// angleAddrRe matches angle-addr tokens including their content.
var angleAddrRe = regexp.MustCompile(`<[^>]*>`)

// sanitizeAddrHeader trims whitespace inside angle-addr brackets.
// Some mailers emit "Name <email >" with a trailing space before the closing
// angle, which net/mail rejects as "unclosed angle-addr". We normalise these
// before parsing so well-known broken senders don't produce noisy errors.
func sanitizeAddrHeader(s string) string {
	if !strings.Contains(s, "<") {
		return s
	}
	return angleAddrRe.ReplaceAllStringFunc(s, func(m string) string {
		return "<" + strings.TrimSpace(m[1:len(m)-1]) + ">"
	})
}

// parseAddress extracts display name and email from a single address header.
func parseAddress(header string) (name, email string) {
	header = sanitizeAddrHeader(strings.TrimSpace(header))
	if header == "" {
		return "", ""
	}
	list, err := enmime.ParseAddressList(header)
	if err != nil || len(list) == 0 {
		slog.Error("parse From header failed", "header", header, "error", err)
		return "", ""
	}
	return list[0].Name, list[0].Address
}

// parseAddresses extracts email addresses from header values.
func parseAddresses(values []string) []string {
	var emails []string
	for _, v := range values {
		v = sanitizeAddrHeader(strings.TrimSpace(v))
		if v == "" {
			continue
		}
		list, err := enmime.ParseAddressList(v)
		if err != nil {
			slog.Error("parse address list failed", "value", v, "error", err)
			continue
		}
		for _, a := range list {
			emails = append(emails, a.Address)
		}
	}
	return emails
}
