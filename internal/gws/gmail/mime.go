package gmail

import (
	"bytes"
	"encoding/base64"
	"fmt"

	"github.com/jhillyerd/enmime"

	"github.com/anish749/pigeon/internal/gws/model"
)

// parseRawMessage decodes a base64url-encoded RFC 2822 message and
// extracts the body text, headers, and attachment metadata using enmime.
func parseRawMessage(raw string) (*parsedMessage, error) {
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("decode raw message: %w", err)
	}

	env, err := enmime.ReadEnvelope(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("parse MIME envelope: %w", err)
	}

	// Prefer plain text; fall back to HTML→text conversion by enmime.
	text := env.Text
	if text == "" {
		text = env.HTML // enmime already provides the raw HTML; plain is better
	}

	fromName, fromAddr := parseAddress(env.GetHeader("From"))
	to := parseAddresses(env.GetHeaderValues("To"))
	cc := parseAddresses(env.GetHeaderValues("Cc"))

	var attachments []model.EmailAttachment
	for _, a := range env.Attachments {
		attachments = append(attachments, model.EmailAttachment{
			ID:   a.ContentID,
			Type: a.ContentType,
			Name: a.FileName,
		})
	}

	return &parsedMessage{
		subject:     env.GetHeader("Subject"),
		fromName:    fromName,
		fromAddr:    fromAddr,
		to:          to,
		cc:          cc,
		text:        text,
		attachments: attachments,
	}, nil
}

type parsedMessage struct {
	subject     string
	fromName    string
	fromAddr    string
	to          []string
	cc          []string
	text        string
	attachments []model.EmailAttachment
}

// parseAddress extracts display name and email from a single address header.
func parseAddress(header string) (name, email string) {
	list, err := enmime.ParseAddressList(header)
	if err != nil || len(list) == 0 {
		return "", header
	}
	return list[0].Name, list[0].Address
}

// parseAddresses extracts email addresses from header values.
func parseAddresses(values []string) []string {
	var emails []string
	for _, v := range values {
		list, err := enmime.ParseAddressList(v)
		if err != nil {
			continue
		}
		for _, a := range list {
			emails = append(emails, a.Address)
		}
	}
	return emails
}
