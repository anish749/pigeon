package commands

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	_ "github.com/mattn/go-sqlite3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/anish/claude-msg-utils/internal/store"
	"github.com/anish/claude-msg-utils/internal/walog"
)

func RunListenWhatsApp(args []string) error {
	fs := flag.NewFlagSet("listen-whatsapp", flag.ExitOnError)
	deviceJID := fs.String("device", "", "device JID from setup-whatsapp [required]")
	dbPath := fs.String("db", "", "SQLite database path (default: <data-dir>/whatsapp.db)")
	account := fs.String("account", "", "account label for directory name (default: phone from JID)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *deviceJID == "" {
		return fmt.Errorf("required flag: -device (get it by running: cmu setup-whatsapp)")
	}
	if *dbPath == "" {
		*dbPath = store.DefaultDBPath()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dsn := fmt.Sprintf("file:%s?_foreign_keys=on", *dbPath)
	container, err := sqlstore.New(ctx, "sqlite3", dsn, walog.New(ctx, "whatsapp-db"))
	if err != nil {
		return fmt.Errorf("create device store: %w", err)
	}

	jid, err := types.ParseJID(*deviceJID)
	if err != nil {
		return fmt.Errorf("parse device JID %q: %w", *deviceJID, err)
	}

	device, err := container.GetDevice(ctx, jid)
	if err != nil {
		return fmt.Errorf("get device for JID %s: %w", jid.String(), err)
	}
	if device == nil {
		return fmt.Errorf("no device found for JID %s — run setup-whatsapp first", jid.String())
	}

	client := whatsmeow.NewClient(device, walog.New(ctx, "whatsapp"))

	// Determine account directory name
	acctName := *account
	if acctName == "" {
		acctName = "+" + jid.User
	}

	client.AddEventHandler(makeWhatsAppHandler(ctx, client, acctName))

	if err := client.Connect(); err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	slog.InfoContext(ctx, "whatsapp listener started", "device", jid.String(), "account", acctName)
	fmt.Printf("Listening for WhatsApp messages (account: %s)...\nPress Ctrl+C to stop.\n", acctName)

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	fmt.Println("\nShutting down...")
	client.Disconnect()
	return nil
}

func makeWhatsAppHandler(ctx context.Context, client *whatsmeow.Client, account string) func(interface{}) {
	return func(evt interface{}) {
		switch v := evt.(type) {
		case *events.Message:
			handleWhatsAppMessage(ctx, client, account, v)
		case *events.Connected:
			slog.InfoContext(ctx, "whatsapp: connected")
		case *events.Disconnected:
			slog.WarnContext(ctx, "whatsapp: disconnected")
		}
	}
}

func handleWhatsAppMessage(ctx context.Context, client *whatsmeow.Client, account string, evt *events.Message) {
	if client.Store.ID == nil {
		return
	}

	// Skip broadcast and self-messages
	if evt.Info.Chat.Server == "broadcast" {
		return
	}
	if evt.Info.Sender.User == client.Store.ID.User {
		return
	}

	text := extractWhatsAppText(evt.Message)
	if text == "" {
		return
	}

	senderJID := evt.Info.Sender
	// Resolve LID to phone JID if needed
	if senderJID.Server == types.HiddenUserServer {
		pnJID, err := client.Store.LIDs.GetPNForLID(ctx, senderJID)
		if err == nil && !pnJID.IsEmpty() {
			senderJID = pnJID
		}
	}

	// Build conversation directory name: +phone_PushName
	phone := "+" + senderJID.User
	pushName := evt.Info.PushName
	if pushName == "" {
		pushName = senderJID.User
	}
	// Sanitize push name for filesystem
	pushName = sanitizeFilename(pushName)
	convDir := phone + "_" + pushName

	senderName := evt.Info.PushName
	if senderName == "" {
		senderName = phone
	}

	if err := store.WriteMessage("whatsapp", account, convDir, senderName, text, evt.Info.Timestamp); err != nil {
		slog.ErrorContext(ctx, "failed to write message", "error", err)
		return
	}

	slog.InfoContext(ctx, "message saved",
		"from", senderName, "conv", convDir, "text_len", len(text))
}

func extractWhatsAppText(msg *waE2E.Message) string {
	if msg == nil {
		return ""
	}
	if msg.Conversation != nil {
		return *msg.Conversation
	}
	if msg.ExtendedTextMessage != nil && msg.ExtendedTextMessage.Text != nil {
		return *msg.ExtendedTextMessage.Text
	}
	if msg.ImageMessage != nil && msg.ImageMessage.Caption != nil {
		return *msg.ImageMessage.Caption
	}
	if msg.VideoMessage != nil && msg.VideoMessage.Caption != nil {
		return *msg.VideoMessage.Caption
	}
	if msg.DocumentMessage != nil && msg.DocumentMessage.Caption != nil {
		return *msg.DocumentMessage.Caption
	}
	return ""
}

func sanitizeFilename(name string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "\x00", "")
	return replacer.Replace(name)
}
