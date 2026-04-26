package whatsapp

import (
	"testing"

	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

func TestEditedMessage(t *testing.T) {
	const origID = "3EB0ABC123"
	newText := "edited body"

	t.Run("MESSAGE_EDIT returns id and new content", func(t *testing.T) {
		msg := &waE2E.Message{
			ProtocolMessage: &waE2E.ProtocolMessage{
				Type: waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(),
				Key:  &waCommon.MessageKey{ID: proto.String(origID)},
				EditedMessage: &waE2E.Message{
					Conversation: proto.String(newText),
				},
			},
		}
		gotID, edited := EditedMessage(msg)
		if gotID != origID {
			t.Errorf("origID = %q, want %q", gotID, origID)
		}
		if got := ExtractText(edited); got != newText {
			t.Errorf("edited text = %q, want %q", got, newText)
		}
	})

	t.Run("REVOKE returns empty", func(t *testing.T) {
		msg := &waE2E.Message{
			ProtocolMessage: &waE2E.ProtocolMessage{
				Type: waE2E.ProtocolMessage_REVOKE.Enum(),
				Key:  &waCommon.MessageKey{ID: proto.String(origID)},
			},
		}
		if id, edited := EditedMessage(msg); id != "" || edited != nil {
			t.Errorf("EditedMessage on REVOKE = (%q, %v), want ('', nil)", id, edited)
		}
	})

	t.Run("plain message returns empty", func(t *testing.T) {
		msg := &waE2E.Message{Conversation: proto.String("hi")}
		if id, edited := EditedMessage(msg); id != "" || edited != nil {
			t.Errorf("EditedMessage on plain text = (%q, %v), want ('', nil)", id, edited)
		}
	})
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
