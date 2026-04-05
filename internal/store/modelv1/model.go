package modelv1

import "time"

// Via represents the message pathway through pigeon.
type Via string

const (
	ViaOrganic      Via = ""               // normal message, user's own connection
	ViaToPigeon     Via = "to-pigeon"      // third party sent this to pigeon's bot
	ViaPigeonAsUser Via = "pigeon-as-user" // pigeon sent using the user's identity
	ViaPigeonAsBot  Via = "pigeon-as-bot"  // pigeon sent using the bot's identity
)

// LineType classifies a parsed line.
type LineType int

const (
	LineMessage    LineType = iota // [id:...]
	LineReaction                   // [react:...]
	LineUnreaction                 // [unreact:...]
	LineEdit                      // [edit:...]
	LineDelete                    // [delete:...]
	LineSeparator                 // --- channel context ---
)

// MsgLine represents a message event.
type MsgLine struct {
	ID          string       // platform message ID
	Ts          time.Time    // message timestamp
	Sender      string       // display name (best-effort at write time)
	SenderID    string       // platform user ID (stable identity)
	Via         Via          // message pathway
	ReplyTo     string       // quoted message ID (WhatsApp quote-reply), empty if not a reply
	Text        string       // message body (may contain newlines)
	Reply       bool         // thread reply (2-space indent)
	Attachments []Attachment // zero or more attachments
}

// Attachment references a file stored in the conversation's attachments/ directory.
type Attachment struct {
	ID   string // platform attachment ID (filename in attachments/)
	Type string // MIME type (e.g. "image/jpeg")
}

// ReactLine represents a reaction or unreaction event.
type ReactLine struct {
	Ts       time.Time // when the reaction happened
	MsgID    string    // target message ID
	Sender   string    // who reacted (display name)
	SenderID string    // who reacted (platform ID)
	Via      Via       // message pathway
	Emoji    string    // emoji name or Unicode character
	Remove   bool      // true = unreact
}

// EditLine represents a message edit event.
type EditLine struct {
	Ts       time.Time // when the edit happened
	MsgID    string    // target message ID
	Sender   string    // who edited (display name)
	SenderID string    // who edited (platform ID)
	Via      Via       // message pathway
	Text     string    // new message text
}

// DeleteLine represents a message delete event.
type DeleteLine struct {
	Ts       time.Time // when the delete happened
	MsgID    string    // target message ID
	Sender   string    // who deleted (display name)
	SenderID string    // who deleted (platform ID)
	Via      Via       // message pathway
}
