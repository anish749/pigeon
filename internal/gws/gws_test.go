package gws

import (
	"fmt"
	"testing"
)

func TestAPIError_Error(t *testing.T) {
	err := &APIError{Code: 404, Message: "not found", Reason: "notFound"}
	want := "gws api 404 notFound: not found"
	if err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
}

func TestIsStatusCode(t *testing.T) {
	apiErr := &APIError{Code: 404, Reason: "notFound", Message: "not found"}
	wrapped := fmt.Errorf("gws call: %w", apiErr)

	if !IsStatusCode(wrapped, 404) {
		t.Error("IsStatusCode(wrapped 404, 404) = false, want true")
	}
	if IsStatusCode(wrapped, 410) {
		t.Error("IsStatusCode(wrapped 404, 410) = true, want false")
	}
	if IsStatusCode(fmt.Errorf("plain error"), 404) {
		t.Error("IsStatusCode(plain error, 404) = true, want false")
	}
}

func TestIsNotFound(t *testing.T) {
	err := fmt.Errorf("gws: %w", &APIError{Code: 404, Reason: "notFound", Message: "gone"})
	if !IsNotFound(err) {
		t.Error("IsNotFound(404) = false, want true")
	}
	if IsNotFound(fmt.Errorf("other")) {
		t.Error("IsNotFound(other) = true, want false")
	}
}

func TestIsGone(t *testing.T) {
	err := fmt.Errorf("gws: %w", &APIError{Code: 410, Reason: "gone", Message: "expired"})
	if !IsGone(err) {
		t.Error("IsGone(410) = false, want true")
	}
}

func TestIsCursorExpired(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"404 notFound", fmt.Errorf("w: %w", &APIError{Code: 404, Reason: "notFound", Message: "m"}), true},
		{"410 gone", fmt.Errorf("w: %w", &APIError{Code: 410, Reason: "gone", Message: "m"}), true},
		{"400 invalid", fmt.Errorf("w: %w", &APIError{Code: 400, Reason: "invalid", Message: "m"}), true},
		{"400 other", fmt.Errorf("w: %w", &APIError{Code: 400, Reason: "badRequest", Message: "m"}), false},
		{"500 server", fmt.Errorf("w: %w", &APIError{Code: 500, Reason: "internal", Message: "m"}), false},
		{"plain error", fmt.Errorf("network timeout"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsCursorExpired(tt.err); got != tt.want {
				t.Errorf("IsCursorExpired(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
