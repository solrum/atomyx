package diag

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestIsTransient(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"unreachable", New(CodeAgentUnreachable, "m", "r", nil), true},
		{"communication failed", New(CodeAgentCommunicationFailed, "m", "r", nil), true},
		{"not ready, wrapped", fmt.Errorf("ctx: %w", New(CodeAgentNotReady, "m", "r", nil)), true},
		{"rejected", New(CodeAgentRejected, "m", "r", nil), false},
		{"uncoded", errors.New("boom"), false},
		{"canceled", context.Canceled, false},
		{"deadline", context.DeadlineExceeded, false},
	}
	for _, tc := range cases {
		if got := IsTransient(tc.err); got != tc.want {
			t.Errorf("%s: IsTransient = %v, want %v", tc.name, got, tc.want)
		}
	}
}
