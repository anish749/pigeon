package api

import (
	"fmt"
	"strings"
)

// SlackTarget identifies a Slack recipient: either a user (for DMs) or a channel/group DM.
// Exactly one of UserID or Channel must be set.
type SlackTarget struct {
	UserID  string `json:"user_id,omitempty"` // Slack user ID (U-prefixed) for DMs
	Channel string `json:"channel,omitempty"` // #channel or @mpdm-... for channels/group DMs
}

// Validate checks that exactly one field is set and that values are well-formed.
func (t SlackTarget) Validate() error {
	if t.UserID == "" && t.Channel == "" {
		return fmt.Errorf("specify user_id or channel")
	}
	if t.UserID != "" && t.Channel != "" {
		return fmt.Errorf("specify user_id or channel, not both")
	}
	if t.UserID != "" && !strings.HasPrefix(t.UserID, "U") {
		return fmt.Errorf("user_id must be a Slack user ID (U-prefixed), got %q", t.UserID)
	}
	if t.Channel != "" && strings.HasPrefix(t.Channel, "@") && !strings.HasPrefix(t.Channel, "@mpdm-") {
		return fmt.Errorf("use user_id for DMs, not channel — run 'pigeon list' to find the user_id")
	}
	return nil
}

// Display returns a human-readable label for the target.
func (t SlackTarget) Display() string {
	if t.UserID != "" {
		return t.UserID
	}
	return t.Channel
}

// ResolvedSlackTarget holds the daemon-resolved Slack destination.
// Exactly one pair is populated: UserID/UserName for DMs,
// ChannelID/ChannelName for channels and group DMs.
type ResolvedSlackTarget struct {
	UserID      string `json:"user_id,omitempty"`
	UserName    string `json:"user_name,omitempty"`
	ChannelID   string `json:"channel_id,omitempty"`
	ChannelName string `json:"channel_name,omitempty"`
}

// Display returns a human-readable label, preferring the resolved name
// and falling back to the raw ID.
func (t ResolvedSlackTarget) Display() string {
	if t.UserName != "" {
		return "@" + t.UserName
	}
	if t.ChannelName != "" {
		return t.ChannelName
	}
	if t.UserID != "" {
		return t.UserID
	}
	return t.ChannelID
}

// Target returns a human-readable label for the send target.
func (r SendRequest) Target() string {
	if r.Slack != nil {
		return r.Slack.Display()
	}
	return r.Contact
}

// ResolvedSendRequest is the daemon-enriched form of a SendRequest.
// Produced after validation and target resolution, this is what gets stored
// in the outbox and deserialized by the review TUI.
type ResolvedSendRequest struct {
	SendRequest
	ResolvedSlack *ResolvedSlackTarget `json:"resolved_slack,omitempty"`
}

// ResolvedTarget returns the human-readable label for the resolved send
// target, falling back to SendRequest.Target() when no resolution exists.
func (r ResolvedSendRequest) ResolvedTarget() string {
	if r.ResolvedSlack != nil {
		return r.ResolvedSlack.Display()
	}
	return r.SendRequest.Target()
}

// validateTarget checks that exactly one of slack or contact is set and matches the platform.
func validateTarget(platform string, slack *SlackTarget, contact string) error {
	hasSlack := slack != nil
	hasContact := contact != ""

	if !hasSlack && !hasContact {
		return fmt.Errorf("specify a target (slack or contact)")
	}
	if hasSlack && hasContact {
		return fmt.Errorf("specify slack or contact, not both")
	}

	switch platform {
	case "slack":
		if !hasSlack {
			return fmt.Errorf("use slack target (user_id or channel) for Slack, not contact")
		}
		return slack.Validate()
	case "whatsapp":
		if !hasContact {
			return fmt.Errorf("use contact for WhatsApp, not slack target")
		}
	}
	return nil
}
