package slackerr

import (
	"errors"
	"fmt"
	"testing"

	goslack "github.com/slack-go/slack"
)

func TestIs(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code string
		want bool
	}{
		{
			name: "matches bare SlackErrorResponse",
			err:  goslack.SlackErrorResponse{Err: "channel_not_found"},
			code: "channel_not_found",
			want: true,
		},
		{
			name: "matches wrapped SlackErrorResponse",
			err:  fmt.Errorf("outer: %w", goslack.SlackErrorResponse{Err: "channel_not_found"}),
			code: "channel_not_found",
			want: true,
		},
		{
			name: "different code does not match",
			err:  goslack.SlackErrorResponse{Err: "not_in_channel"},
			code: "channel_not_found",
			want: false,
		},
		{
			name: "plain error with same text does not match",
			err:  errors.New("channel_not_found"),
			code: "channel_not_found",
			want: false,
		},
		{
			name: "nil error",
			err:  nil,
			code: "channel_not_found",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Is(tt.err, tt.code); got != tt.want {
				t.Errorf("Is(%v, %q) = %v, want %v", tt.err, tt.code, got, tt.want)
			}
		})
	}
}

func TestIsChannelNotFound(t *testing.T) {
	if !IsChannelNotFound(goslack.SlackErrorResponse{Err: "channel_not_found"}) {
		t.Error("expected match for bare channel_not_found")
	}
	if !IsChannelNotFound(fmt.Errorf("wrap: %w", goslack.SlackErrorResponse{Err: "channel_not_found"})) {
		t.Error("expected match for wrapped channel_not_found")
	}
	if IsChannelNotFound(goslack.SlackErrorResponse{Err: "is_archived"}) {
		t.Error("expected no match for is_archived")
	}
	if IsChannelNotFound(errors.New("channel_not_found")) {
		t.Error("expected no match for plain error")
	}
}
