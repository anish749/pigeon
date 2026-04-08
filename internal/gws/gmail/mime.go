package gmail

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"log/slog"

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

	// env.Text is always populated — either from the text/plain part
	// or from enmime's HTML→text conversion. env.HTML is only populated
	// when a multipart message has an explicit text/html part.
	//
	// We store text always (greppable). We store html when present so
	// the protocol carries enough info to render rich content later.
	return &parsedMessage{
		subject:     env.GetHeader("Subject"),
		fromName:    fromName,
		fromAddr:    fromAddr,
		to:          to,
		cc:          cc,
		text:        env.Text,
		html:        env.HTML,
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
	html        string
	attachments []model.EmailAttachment
}

// parseAddress extracts display name and email from a single address header.
func parseAddress(header string) (name, email string) {
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
