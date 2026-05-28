package ccview

import (
	"encoding/json"
	"testing"

	goslack "github.com/slack-go/slack"

	"github.com/anish749/pigeon/internal/outbox"
	"github.com/anish749/pigeon/internal/store/modelv1"
)

func makeItem(via modelv1.Via, sessionID string) *outbox.Item {
	payload, _ := json.Marshal(map[string]string{
		"platform": "slack",
		"via":      string(via),
	})
	return &outbox.Item{ID: "item-1", SessionID: sessionID, Payload: payload}
}

func TestFromItem(t *testing.T) {
	item := makeItem(modelv1.ViaPigeonAsUser, "sess-1")
	v := FromItem(item, "hello world", "to #eng")

	if v.Message != "hello world" {
		t.Fatalf("Message = %q", v.Message)
	}
	if v.Target != "to #eng" {
		t.Fatalf("Target = %q", v.Target)
	}
	if v.ItemID != "item-1" {
		t.Fatalf("ItemID = %q", v.ItemID)
	}
	if v.SessionID != "sess-1" {
		t.Fatalf("SessionID = %q", v.SessionID)
	}
	if v.Via != modelv1.ViaPigeonAsUser {
		t.Fatalf("Via = %q", v.Via)
	}
}

func TestFromBlocks(t *testing.T) {
	blocks := []goslack.Block{
		goslack.NewSectionBlock(
			goslack.NewTextBlockObject("mrkdwn", "test message", false, false), nil, nil,
		),
		goslack.NewContextBlock("",
			goslack.NewTextBlockObject("mrkdwn", "to #general", false, false),
		),
		&goslack.ActionBlock{Type: "actions", BlockID: "outbox_actions"},
	}

	v := FromBlocks(blocks)
	if v.Message != "test message" {
		t.Fatalf("Message = %q, want %q", v.Message, "test message")
	}
	if v.Target != "to #general" {
		t.Fatalf("Target = %q, want %q", v.Target, "to #general")
	}
	if v.ItemID != "" || v.SessionID != "" {
		t.Fatal("FromBlocks should not populate ItemID or SessionID")
	}
}

func TestFromBlocksEmpty(t *testing.T) {
	v := FromBlocks(nil)
	if v.Message != "" || v.Target != "" {
		t.Fatalf("FromBlocks(nil) should return empty View, got Message=%q Target=%q", v.Message, v.Target)
	}
}

func TestBlocksWithSession(t *testing.T) {
	v := View{Message: "hi", Target: "to #eng", ItemID: "x", SessionID: "s1", Via: modelv1.ViaPigeonAsBot}
	blocks := v.Blocks()

	// section + context + actions = 3 blocks
	if len(blocks) != 3 {
		t.Fatalf("len(Blocks()) = %d, want 3", len(blocks))
	}
	if blocks[0].BlockType() != goslack.MBTSection {
		t.Fatalf("blocks[0] type = %s, want section", blocks[0].BlockType())
	}
	if blocks[1].BlockType() != goslack.MBTContext {
		t.Fatalf("blocks[1] type = %s, want context", blocks[1].BlockType())
	}
	if blocks[2].BlockType() != goslack.MBTAction {
		t.Fatalf("blocks[2] type = %s, want actions", blocks[2].BlockType())
	}

	// With session: 4 buttons (approve, dismiss, sendmode, feedback)
	actions := blocks[2].(*goslack.ActionBlock)
	if len(actions.Elements.ElementSet) != 4 {
		t.Fatalf("button count = %d, want 4", len(actions.Elements.ElementSet))
	}
	assertButtonAction(t, actions.Elements.ElementSet[0], "outbox_approve")
	assertButtonAction(t, actions.Elements.ElementSet[1], "outbox_dismiss")
	assertButtonAction(t, actions.Elements.ElementSet[2], "outbox_sendmode")
	assertButtonAction(t, actions.Elements.ElementSet[3], "outbox_feedback")
}

func TestBlocksWithoutSession(t *testing.T) {
	v := View{Message: "hi", Target: "to #eng", ItemID: "x", SessionID: "", Via: modelv1.ViaPigeonAsBot}
	blocks := v.Blocks()

	actions := blocks[2].(*goslack.ActionBlock)
	// No session: 3 buttons (no feedback)
	if len(actions.Elements.ElementSet) != 3 {
		t.Fatalf("button count = %d, want 3 (no feedback)", len(actions.Elements.ElementSet))
	}
	for _, el := range actions.Elements.ElementSet {
		btn := el.(*goslack.ButtonBlockElement)
		if btn.ActionID == "outbox_feedback" {
			t.Fatal("feedback button should not be present without session")
		}
	}
}

func TestBlocksSendModeLabel(t *testing.T) {
	for _, tt := range []struct {
		via  modelv1.Via
		want string
	}{
		{"", "Send as: bot"},
		{modelv1.ViaPigeonAsBot, "Send as: bot"},
		{modelv1.ViaPigeonAsUser, "Send as: user"},
	} {
		v := View{Message: "m", ItemID: "x", Via: tt.via}
		actions := v.Blocks()[1].(*goslack.ActionBlock) // no target → section + actions
		sendBtn := actions.Elements.ElementSet[2].(*goslack.ButtonBlockElement)
		if sendBtn.Text.Text != tt.want {
			t.Errorf("via=%q: sendmode label = %q, want %q", tt.via, sendBtn.Text.Text, tt.want)
		}
	}
}

func TestStatusBlocks(t *testing.T) {
	v := View{Message: "hello", Target: "to #eng"}
	blocks := v.StatusBlocks("✓ Approved")

	// section + target context + status context = 3
	if len(blocks) != 3 {
		t.Fatalf("len(StatusBlocks()) = %d, want 3", len(blocks))
	}
	if blocks[2].BlockType() != goslack.MBTContext {
		t.Fatalf("status block type = %s, want context", blocks[2].BlockType())
	}
	ctx := blocks[2].(*goslack.ContextBlock)
	txt := ctx.ContextElements.Elements[0].(*goslack.TextBlockObject)
	if txt.Text != "✓ Approved" {
		t.Fatalf("status text = %q, want %q", txt.Text, "✓ Approved")
	}
}

func TestStatusBlocksEmptyView(t *testing.T) {
	v := View{}
	blocks := v.StatusBlocks("status")
	// No message, no target → just the status context block
	if len(blocks) != 1 {
		t.Fatalf("len = %d, want 1", len(blocks))
	}
}

func TestFallbackText(t *testing.T) {
	v := View{Message: "reschedule meeting"}
	if got := v.FallbackText(); got != "Pending review: reschedule meeting" {
		t.Fatalf("FallbackText() = %q", got)
	}
}

func TestSendModeLabel(t *testing.T) {
	if got := SendModeLabel(""); got != "Send as: bot" {
		t.Fatalf("SendModeLabel('') = %q", got)
	}
	if got := SendModeLabel(modelv1.ViaPigeonAsBot); got != "Send as: bot" {
		t.Fatalf("SendModeLabel(bot) = %q", got)
	}
	if got := SendModeLabel(modelv1.ViaPigeonAsUser); got != "Send as: user" {
		t.Fatalf("SendModeLabel(user) = %q", got)
	}
}

func TestBlocksRoundTrip(t *testing.T) {
	item := makeItem(modelv1.ViaPigeonAsUser, "sess-1")
	original := FromItem(item, "test msg", "to #eng")

	// Build blocks, then extract back
	blocks := original.Blocks()
	extracted := FromBlocks(blocks)

	if extracted.Message != original.Message {
		t.Fatalf("Message round-trip: %q != %q", extracted.Message, original.Message)
	}
	if extracted.Target != original.Target {
		t.Fatalf("Target round-trip: %q != %q", extracted.Target, original.Target)
	}
}

func assertButtonAction(t *testing.T, el goslack.BlockElement, wantAction string) {
	t.Helper()
	btn, ok := el.(*goslack.ButtonBlockElement)
	if !ok {
		t.Fatalf("expected ButtonBlockElement, got %T", el)
	}
	if btn.ActionID != wantAction {
		t.Fatalf("button ActionID = %q, want %q", btn.ActionID, wantAction)
	}
}
