package whatsapp

import (
	"context"
	"log/slog"

	"go.mau.fi/whatsmeow/proto/waE2E"
	wastore "go.mau.fi/whatsmeow/store"
	"google.golang.org/protobuf/proto"
)

// requestResyncIfNeeded checks whether the local message store is empty for
// this account. If the device is paired but no conversations exist locally,
// it sends a FULL_HISTORY_SYNC_ON_DEMAND peer message to the primary device.
//
// This covers the case where `pigeon reset` wiped message data while the
// device remained paired — WhatsApp won't re-send history automatically
// because it considers the device already synced.
//
// Known limitation: if the daemon crashes mid-history-sync, the store will
// contain partial data. On next connect the store is non-empty, so no resync
// is requested, and WhatsApp won't resend the initial history. This is
// accepted as rare enough that the added complexity of tracking sync
// completion isn't warranted.
func (l *Listener) requestResyncIfNeeded(ctx context.Context) {
	convs, err := l.store.ListConversations(l.acct)
	if err != nil {
		slog.ErrorContext(ctx, "whatsapp: failed to list conversations for resync check",
			"account", l.acct, "error", err)
		return
	}
	if len(convs) > 0 {
		return // store has data — nothing to do
	}

	slog.InfoContext(ctx, "whatsapp: empty store detected, requesting full history sync",
		"account", l.acct)

	msg := buildFullHistorySyncRequest()
	if _, err := l.client.SendPeerMessage(ctx, msg); err != nil {
		slog.ErrorContext(ctx, "whatsapp: failed to request history re-sync",
			"account", l.acct, "error", err)
		return
	}

	slog.InfoContext(ctx, "whatsapp: full history re-sync requested",
		"account", l.acct)
}

// buildFullHistorySyncRequest constructs a FULL_HISTORY_SYNC_ON_DEMAND peer
// message. The primary device responds with history sync notifications that
// the existing handleHistorySync handler processes.
func buildFullHistorySyncRequest() *waE2E.Message {
	return &waE2E.Message{
		ProtocolMessage: &waE2E.ProtocolMessage{
			Type: waE2E.ProtocolMessage_PEER_DATA_OPERATION_REQUEST_MESSAGE.Enum(),
			PeerDataOperationRequestMessage: &waE2E.PeerDataOperationRequestMessage{
				PeerDataOperationRequestType: waE2E.PeerDataOperationRequestType_FULL_HISTORY_SYNC_ON_DEMAND.Enum(),
				FullHistorySyncOnDemandRequest: &waE2E.PeerDataOperationRequestMessage_FullHistorySyncOnDemandRequest{
					HistorySyncConfig: wastore.DeviceProps.HistorySyncConfig,
					FullHistorySyncOnDemandConfig: &waE2E.FullHistorySyncOnDemandConfig{
						HistoryDurationDays: proto.Uint32(365),
					},
				},
			},
		},
	}
}
