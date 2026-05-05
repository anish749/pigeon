package poller

import (
	"errors"
	"fmt"
	"testing"

	jira "github.com/ankitpokhrel/jira-cli/pkg/jira"
)

func TestIs404(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"plain error", fmt.Errorf("network is unreachable"), false},
		{"wrapped 500", fmt.Errorf("v3 search: %w", &jira.ErrUnexpectedResponse{StatusCode: 500}), false},
		{"wrapped 401", fmt.Errorf("v3 search: %w", &jira.ErrUnexpectedResponse{StatusCode: 401}), false},
		{"wrapped 404", fmt.Errorf("fetch ENG-1: %w", &jira.ErrUnexpectedResponse{StatusCode: 404}), true},
		{"bare 404", &jira.ErrUnexpectedResponse{StatusCode: 404}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := is404(c.err); got != c.want {
				t.Errorf("is404(%v) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}

// TestIs404UsesErrorsAs is a sanity check that is404 unwraps via
// errors.As (not type assertion), so multi-layer wrapping still works.
func TestIs404UsesErrorsAs(t *testing.T) {
	deep := fmt.Errorf("layer 1: %w", fmt.Errorf("layer 2: %w", &jira.ErrUnexpectedResponse{StatusCode: 404}))
	if !is404(deep) {
		t.Error("is404 did not unwrap multi-layer wrapping")
	}
	// errors.Is on a sentinel is irrelevant here, but confirm errors.As
	// finds the type.
	var unexpected *jira.ErrUnexpectedResponse
	if !errors.As(deep, &unexpected) {
		t.Error("errors.As did not find ErrUnexpectedResponse")
	}
	if unexpected.StatusCode != 404 {
		t.Errorf("unwrapped StatusCode = %d, want 404", unexpected.StatusCode)
	}
}
