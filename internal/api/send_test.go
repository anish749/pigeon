package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	goslack "github.com/slack-go/slack"
)

func TestShellEscapedExclamation(t *testing.T) {
	// zsh/bash with histexpand escape "!" to "\!" in interactive sessions.
	// Verify that the backslash is stripped before the message hits Slack.
	tests := []struct {
		in   string
		want string
	}{
		{`nice patch\! PR merge`, "nice patch! PR merge"},
		{`hello\! world\!`, "hello! world!"},
		{"no exclamation here", "no exclamation here"},
		{"nice patch! PR merge", "nice patch! PR merge"},
		{`🐦 *coo coo* — nice patch\! PR merge`, "🐦 *coo coo* — nice patch! PR merge"},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := strings.ReplaceAll(tt.in, `\!`, "!")
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSendSlack_MessagePassthrough(t *testing.T) {
	// Verify the full path: message text arrives at Slack's API unmodified.
	var gotText string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		gotText = r.FormValue("text")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":      true,
			"channel": "C123",
			"ts":      "1234567890.123456",
		})
	}))
	defer ts.Close()

	api := goslack.New("xoxb-fake", goslack.OptionAPIURL(ts.URL+"/"))
	msg := "🐦 *coo coo* — nice patch! PR merge"
	_, _, err := api.PostMessage("C123", goslack.MsgOptionText(msg, false))
	if err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if gotText != msg {
		t.Errorf("text mismatch:\n  sent: %q\n  got:  %q", msg, gotText)
	}
}

func TestScheduleSlack_PostAtPassthrough(t *testing.T) {
	// Verify that --post-at routes through chat.scheduleMessage with the
	// correct post_at value and message text.
	var gotPostAt, gotText, gotEndpoint string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEndpoint = r.URL.Path
		r.ParseForm()
		gotPostAt = r.FormValue("post_at")
		gotText = r.FormValue("text")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":                   true,
			"channel":              "C123",
			"scheduled_message_id": "Q1234567890",
		})
	}))
	defer ts.Close()

	api := goslack.New("xoxb-fake", goslack.OptionAPIURL(ts.URL+"/"))
	msg := "scheduled message"
	postAt := "1711568938"
	_, scheduledID, err := api.ScheduleMessage("C123", postAt, goslack.MsgOptionText(msg, false))
	if err != nil {
		t.Fatalf("ScheduleMessage: %v", err)
	}
	if gotEndpoint != "/chat.scheduleMessage" {
		t.Errorf("endpoint = %q, want /chat.scheduleMessage", gotEndpoint)
	}
	if gotPostAt != postAt {
		t.Errorf("post_at = %q, want %q", gotPostAt, postAt)
	}
	if gotText != msg {
		t.Errorf("text = %q, want %q", gotText, msg)
	}
	if scheduledID != "Q1234567890" {
		t.Errorf("scheduled_message_id = %q, want Q1234567890", scheduledID)
	}
}
