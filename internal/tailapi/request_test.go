package tailapi

import (
	"encoding/base64"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/account"
)

func TestRequest_IsZero(t *testing.T) {
	tests := []struct {
		name string
		req  Request
		want bool
	}{
		{
			name: "zero value is zero",
			req:  Request{},
			want: true,
		},
		{
			name: "nil accounts with zero time is zero",
			req:  Request{Accounts: nil, Since: time.Time{}},
			want: true,
		},
		{
			name: "empty accounts slice with zero time is zero",
			req:  Request{Accounts: []account.Account{}, Since: time.Time{}},
			want: true,
		},
		{
			name: "with accounts is not zero",
			req:  Request{Accounts: []account.Account{account.New("slack", "acme")}},
			want: false,
		},
		{
			name: "with since is not zero",
			req:  Request{Since: time.Unix(1, 0)},
			want: false,
		},
		{
			name: "with both is not zero",
			req: Request{
				Accounts: []account.Account{account.New("slack", "acme")},
				Since:    time.Unix(1, 0),
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.req.IsZero(); got != tt.want {
				t.Errorf("IsZero() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRequest_RoundTrip(t *testing.T) {
	// Fixed timestamp so equality comparisons are stable. UTC so the
	// JSON encoding doesn't depend on the test host's timezone.
	ts := time.Date(2026, 4, 23, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		req  Request
	}{
		{
			name: "empty request",
			req:  Request{},
		},
		{
			name: "single account",
			req: Request{
				Accounts: []account.Account{account.New("slack", "acme")},
			},
		},
		{
			name: "multiple accounts across platforms",
			req: Request{
				Accounts: []account.Account{
					account.New("slack", "acme"),
					account.New("slack", "other-workspace"),
					account.New("whatsapp", "+14155551234"),
				},
			},
		},
		{
			name: "since only",
			req:  Request{Since: ts},
		},
		{
			name: "accounts and since together",
			req: Request{
				Accounts: []account.Account{account.New("slack", "acme")},
				Since:    ts,
			},
		},
		{
			name: "account name with special characters",
			req: Request{
				Accounts: []account.Account{
					account.New("slack", "name with spaces & symbols!?"),
				},
			},
		},
		{
			name: "account name with unicode",
			req: Request{
				Accounts: []account.Account{
					account.New("slack", "café-团队"),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := tt.req.Encode()
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}

			got, err := Decode(q)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}

			// Normalize nil vs empty slice for the zero-request case —
			// json.Unmarshal never produces []account.Account{} so we
			// compare against the normalized form.
			want := tt.req
			if len(want.Accounts) == 0 {
				want.Accounts = nil
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("round trip mismatch:\n got: %+v\nwant: %+v", got, want)
			}
		})
	}
}

func TestRequest_Encode(t *testing.T) {
	tests := []struct {
		name      string
		req       Request
		wantEmpty bool // expect no query keys at all
	}{
		{
			name:      "zero request encodes to empty values",
			req:       Request{},
			wantEmpty: true,
		},
		{
			name: "non-zero request produces single q key",
			req: Request{
				Accounts: []account.Account{account.New("slack", "acme")},
			},
			wantEmpty: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := tt.req.Encode()
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}

			if tt.wantEmpty {
				if len(q) != 0 {
					t.Errorf("expected empty values, got %v", q)
				}
				return
			}

			if got := len(q); got != 1 {
				t.Errorf("expected exactly 1 query key, got %d (%v)", got, q)
			}
			if q.Get(QueryParam) == "" {
				t.Errorf("expected %q key to be set, got %v", QueryParam, q)
			}
		})
	}
}

func TestDecode_Errors(t *testing.T) {
	tests := []struct {
		name      string
		values    url.Values
		wantErr   bool
		wantEmpty bool // result should be a zero Request
	}{
		{
			name:      "empty values returns zero request",
			values:    url.Values{},
			wantEmpty: true,
		},
		{
			name:      "missing q key returns zero request",
			values:    url.Values{"other": []string{"value"}},
			wantEmpty: true,
		},
		{
			name:    "invalid base64 returns error",
			values:  url.Values{QueryParam: []string{"!!!not-base64!!!"}},
			wantErr: true,
		},
		{
			name: "invalid JSON returns error",
			values: url.Values{
				QueryParam: []string{base64.RawURLEncoding.EncodeToString([]byte("not json"))},
			},
			wantErr: true,
		},
		{
			name: "valid base64 of empty JSON object decodes to zero request",
			values: url.Values{
				QueryParam: []string{base64.RawURLEncoding.EncodeToString([]byte("{}"))},
			},
			wantEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Decode(tt.values)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got Request=%+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantEmpty && !got.IsZero() {
				t.Errorf("expected zero Request, got %+v", got)
			}
		})
	}
}

func TestDecode_ErrorMessages(t *testing.T) {
	// Error messages are part of the debugging contract for operators
	// reading daemon logs. Pin the key substrings so we notice if they
	// silently change.
	tests := []struct {
		name           string
		values         url.Values
		wantContaining string
	}{
		{
			name:           "base64 error mentions decode",
			values:         url.Values{QueryParam: []string{"!!!"}},
			wantContaining: "decode tail request",
		},
		{
			name: "json error mentions unmarshal",
			values: url.Values{
				QueryParam: []string{base64.RawURLEncoding.EncodeToString([]byte("not json"))},
			},
			wantContaining: "unmarshal tail request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Decode(tt.values)
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantContaining) {
				t.Errorf("error %q does not contain %q", err, tt.wantContaining)
			}
		})
	}
}

func TestEncode_QueryParamStability(t *testing.T) {
	// Pin the wire format: if a future change alters QueryParam or the
	// base64/JSON choice, this test forces the author to notice and
	// decide whether it's a breaking change for existing daemons.
	req := Request{
		Accounts: []account.Account{account.New("slack", "acme")},
	}
	q, err := req.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	raw := q.Get(QueryParam)
	if raw == "" {
		t.Fatalf("expected %q to be set", QueryParam)
	}

	// The value must be valid RawURL base64.
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		t.Fatalf("value under %q is not RawURL base64: %v", QueryParam, err)
	}

	// The decoded payload must start with '{' (JSON object).
	if len(decoded) == 0 || decoded[0] != '{' {
		t.Errorf("decoded payload is not a JSON object: %q", decoded)
	}
}
