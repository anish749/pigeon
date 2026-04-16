package gws

import (
	"errors"
	"fmt"
	"strings"
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

func TestParseExitError(t *testing.T) {
	tests := []struct {
		name       string
		stdout     string
		stderr     string
		wantCode   int    // expected APIError.Code, 0 means no APIError
		wantSubstr string // substring expected in error message
	}{
		{
			name:     "structured error on stdout",
			stdout:   `{"error":{"code":404,"message":"not found","reason":"notFound"}}`,
			stderr:   "Using keyring backend: keyring\n",
			wantCode: 404,
		},
		{
			name:     "expired cursor 410",
			stdout:   `{"error":{"code":410,"message":"expired","reason":"gone"}}`,
			stderr:   "Using keyring backend: keyring\n",
			wantCode: 410,
		},
		{
			name:       "unstructured stdout falls back",
			stdout:     "some unexpected output",
			stderr:     "Using keyring backend: keyring\n",
			wantSubstr: "some unexpected output",
		},
		{
			name:       "empty stdout falls back to stderr",
			stdout:     "",
			stderr:     "ENOENT: no such file or directory, uv_cwd\n",
			wantSubstr: "ENOENT",
		},
		{
			name:       "both empty",
			stdout:     "",
			stderr:     "",
			wantSubstr: "gws",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := parseExitError([]string{"test"}, []byte(tt.stdout), []byte(tt.stderr))
			if tt.wantCode != 0 {
				var apiErr *APIError
				if !errors.As(err, &apiErr) {
					t.Fatalf("expected APIError, got %T: %v", err, err)
				}
				if apiErr.Code != tt.wantCode {
					t.Errorf("code = %d, want %d", apiErr.Code, tt.wantCode)
				}
			} else if tt.wantSubstr != "" {
				if got := err.Error(); !strings.Contains(got, tt.wantSubstr) {
					t.Errorf("error = %q, want substring %q", got, tt.wantSubstr)
				}
			}
		})
	}
}

