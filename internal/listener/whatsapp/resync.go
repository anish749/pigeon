package whatsapp

import (
	"context"
	"log/slog"
	"os"

	"go.mau.fi/whatsmeow/proto/waE2E"
	wastore "go.mau.fi/whatsmeow/store"
	"google.golang.org/protobuf/proto"

	"github.com/anish749/pigeon/internal/paths"
)

// requestResyncIfNeeded checks for a resync marker left by `pigeon reset`
// and, if present, sends a full history sync request to the primary device.
// The marker is removed after the request is sent so it only fires once.
func (l *Listener) requestResyncIfNeeded(ctx context.Context) {
	acctDir := paths.DefaultDataRoot().AccountFor(l.acct)
	marker := acctDir.ResyncMarkerPath()

	if _, err := os.Stat(marker); err != nil {
		return // no marker — nothing to do
	}

	slog.InfoContext(ctx, "whatsapp: resync marker found, requesting full history sync",
		"account", l.acct)

	msg := buildFullHistorySyncRequest()
	if _, err := l.client.SendPeerMessage(ctx, msg); err != nil {
		slog.ErrorContext(ctx, "whatsapp: failed to request history re-sync",
			"account", l.acct, "error", err)
		return
	}

	if err := os.Remove(marker); err != nil {
		slog.WarnContext(ctx, "whatsapp: failed to remove resync marker",
			"account", l.acct, "error", err)
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
