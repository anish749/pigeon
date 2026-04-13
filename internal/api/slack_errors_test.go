package api

import (
	"errors"
	"fmt"
	"testing"

	goslack "github.com/slack-go/slack"
)

func TestSlackChannelNotFoundHint(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantHint bool
	}{
		{
			name:     "channel_not_found",
			err:      goslack.SlackErrorResponse{Err: "channel_not_found"},
			wantHint: true,
		},
		{
			name:     "wrapped channel_not_found",
			err:      fmt.Errorf("outer: %w", goslack.SlackErrorResponse{Err: "channel_not_found"}),
			wantHint: true,
		},
		{
			name:     "other slack error",
			err:      goslack.SlackErrorResponse{Err: "not_in_channel"},
			wantHint: false,
		},
		{
			name:     "non-slack error",
			err:      errors.New("channel_not_found"),
			wantHint: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := slackChannelNotFoundHint(tt.err)
			if tt.wantHint && got == "" {
				t.Error("expected hint, got empty string")
			}
			if !tt.wantHint && got != "" {
				t.Errorf("expected no hint, got %q", got)
			}
		})
	}
}
