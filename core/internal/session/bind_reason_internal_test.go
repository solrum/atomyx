package session

import (
	"errors"
	"fmt"
	"io/fs"
	"syscall"
	"testing"

	"dev.atomyx/internal/attach"
)

func TestBindFailureReason_NamesTheCauseWithoutAPath(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"unsafe directory", fmt.Errorf("attach: socket directory /run/x is open: %w", attach.ErrUnsafeSocketDir), reasonUnsafeDir},
		{"permission", fmt.Errorf("listen /run/x/a.sock: %w", fs.ErrPermission), reasonDenied},
		{"address in use", fmt.Errorf("listen /run/x/a.sock: %w", syscall.EADDRINUSE), reasonInUse},
		{"path too long", fmt.Errorf("listen /run/x/a.sock: %w", syscall.EINVAL), reasonPathTooLong},
		{"anything else", errors.New("boom at /run/x/a.sock"), reasonBindFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := bindFailureReason(tc.err); got != tc.want {
				t.Fatalf("bindFailureReason() = %q, want %q", got, tc.want)
			}
		})
	}
}
