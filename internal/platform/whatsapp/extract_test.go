package whatsapp

import (
	"testing"

	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

func TestEditedMessage(t *testing.T) {
	const origID = "3EB0ABC123"
	const newText = "edited body"

	tests := []struct {
		name     string
		msg      *waE2E.Message
		wantID   string
		wantText string // text expected from ExtractText(edited); "" when edited is nil
	}{
		{
			name: "MESSAGE_EDIT returns id and new content",
			msg: &waE2E.Message{
				ProtocolMessage: &waE2E.ProtocolMessage{
					Type:          waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(),
					Key:           &waCommon.MessageKey{ID: proto.String(origID)},
					EditedMessage: &waE2E.Message{Conversation: proto.String(newText)},
				},
			},
			wantID:   origID,
			wantText: newText,
		},
		{
			name: "MESSAGE_EDIT with caption-only image edit",
			msg: &waE2E.Message{
				ProtocolMessage: &waE2E.ProtocolMessage{
					Type: waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(),
					Key:  &waCommon.MessageKey{ID: proto.String(origID)},
					EditedMessage: &waE2E.Message{
						ImageMessage: &waE2E.ImageMessage{Caption: proto.String("new caption")},
					},
				},
			},
			wantID:   origID,
			wantText: "new caption",
		},
		{
			name: "REVOKE returns empty",
			msg: &waE2E.Message{
				ProtocolMessage: &waE2E.ProtocolMessage{
					Type: waE2E.ProtocolMessage_REVOKE.Enum(),
					Key:  &waCommon.MessageKey{ID: proto.String(origID)},
				},
			},
		},
		{
			name: "plain text message returns empty",
			msg:  &waE2E.Message{Conversation: proto.String("hi")},
		},
		{
			name: "nil message returns empty",
			msg:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotID, edited := EditedMessage(tt.msg)
			if gotID != tt.wantID {
				t.Errorf("origID = %q, want %q", gotID, tt.wantID)
			}
			if tt.wantText == "" {
				if edited != nil {
					t.Errorf("edited = %v, want nil", edited)
				}
				return
			}
			if got := ExtractText(edited); got != tt.wantText {
				t.Errorf("edited text = %q, want %q", got, tt.wantText)
			}
		})
	}
}

func TestRevokedMessageID(t *testing.T) {
	const origID = "3EB0DEADBEEF"

	tests := []struct {
		name string
		msg  *waE2E.Message
		want string
	}{
		{
			name: "REVOKE returns target id",
			msg: &waE2E.Message{
				ProtocolMessage: &waE2E.ProtocolMessage{
					Type: waE2E.ProtocolMessage_REVOKE.Enum(),
					Key:  &waCommon.MessageKey{ID: proto.String(origID)},
				},
			},
			want: origID,
		},
		{
			name: "MESSAGE_EDIT returns empty",
			msg: &waE2E.Message{
				ProtocolMessage: &waE2E.ProtocolMessage{
					Type: waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(),
					Key:  &waCommon.MessageKey{ID: proto.String(origID)},
				},
			},
			want: "",
		},
		{
			name: "no protocol message returns empty",
			msg:  &waE2E.Message{Conversation: proto.String("hi")},
			want: "",
		},
		{
			name: "nil message returns empty",
			msg:  nil,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RevokedMessageID(tt.msg); got != tt.want {
				t.Errorf("RevokedMessageID = %q, want %q", got, tt.want)
			}
		})
	}
}
