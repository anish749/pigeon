package gws

import (
	"bytes"
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

func TestTrimToJSON(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"keyring prefix on object", "Using keyring backend: keyring\n{\"ok\":true}", "{\"ok\":true}"},
		{"keyring prefix on array", "Using keyring backend: keyring\n[1,2,3]", "[1,2,3]"},
		{"no prefix object", "{\"ok\":true}", "{\"ok\":true}"},
		{"no prefix array", "[1,2,3]", "[1,2,3]"},
		{"object before array", "noise {\"a\":1} [2]", "{\"a\":1} [2]"},
		{"array before object", "noise [1] {\"a\":1}", "[1] {\"a\":1}"},
		{"no json bytes", "just text", "just text"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TrimToJSON([]byte(tt.in))
			if !bytes.Equal(got, []byte(tt.want)) {
				t.Errorf("TrimToJSON(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
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
