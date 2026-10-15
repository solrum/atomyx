package androiddriver_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"dev.atomyx/internal/androiddriver"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/process"
	"dev.atomyx/internal/transport"
)

func TestAndroidDriver_TypedRejections_KeepReasonAndRemedy(t *testing.T) {
	const leakingReason = "failed calling http://127.0.0.1:8765/actions/force_stop"
	cases := []struct {
		name       string
		configure  func(tr *fakeTransport)
		call       func(d *androiddriver.AndroidDriver) error
		original   func(err error) bool
		wantMsg    string
		wantRemedy string
	}{
		{
			name: "clear text with reason",
			configure: func(tr *fakeTransport) {
				tr.clearFocusedResp = transport.ClearFocusedInputResult{Ok: false, Reason: "field is read-only"}
			},
			call: func(d *androiddriver.AndroidDriver) error { return d.EraseText(context.Background(), 3) },
			original: func(err error) bool {
				var e *androiddriver.ClearTextError
				return errors.As(err, &e)
			},
			wantMsg:    "field is read-only",
			wantRemedy: "Focus an input field that holds text, then call again.",
		},
		{
			name: "clear text without reason",
			configure: func(tr *fakeTransport) {
				tr.clearFocusedResp = transport.ClearFocusedInputResult{Ok: false}
			},
			call: func(d *androiddriver.AndroidDriver) error { return d.EraseText(context.Background(), 3) },
			original: func(err error) bool {
				var e *androiddriver.ClearTextError
				return errors.As(err, &e)
			},
			wantMsg:    "the focused input could not be cleared",
			wantRemedy: "Focus an input field that holds text, then call again.",
		},
		{
			name: "force stop with reason",
			configure: func(tr *fakeTransport) {
				tr.forceStopResp = transport.AckResult{Ok: false, Reason: "not permitted to stop com.example.app"}
			},
			call: func(d *androiddriver.AndroidDriver) error { return d.StopApp(context.Background(), "com.example.app") },
			original: func(err error) bool {
				var e *androiddriver.ForceStopError
				return errors.As(err, &e)
			},
			wantMsg:    "not permitted to stop com.example.app",
			wantRemedy: "Confirm the app is installed and the device is connected, then call again. The app was not stopped.",
		},
		{
			name: "force stop without reason",
			configure: func(tr *fakeTransport) {
				tr.forceStopResp = transport.AckResult{Ok: false}
			},
			call: func(d *androiddriver.AndroidDriver) error { return d.StopApp(context.Background(), "com.example.app") },
			original: func(err error) bool {
				var e *androiddriver.ForceStopError
				return errors.As(err, &e)
			},
			wantMsg:    "the app could not be force-stopped",
			wantRemedy: "Confirm the app is installed and the device is connected, then call again. The app was not stopped.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := newFakeTransport()
			tc.configure(tr)
			d := androiddriver.New(tr, process.NewFake(), androiddriver.Options{Serial: "S1"})

			err := tc.call(d)
			if err == nil {
				t.Fatal("call returned no error")
			}
			if !tc.original(err) {
				t.Fatalf("errors.As no longer finds the original typed error in %v (%T)", err, err)
			}
			code, msg, remedy := diag.Describe(err)
			if code != diag.CodeAgentRejected {
				t.Fatalf("code = %q, want %q", code, diag.CodeAgentRejected)
			}
			if msg != tc.wantMsg {
				t.Fatalf("message = %q, want %q", msg, tc.wantMsg)
			}
			if remedy != tc.wantRemedy {
				t.Fatalf("remedy = %q, want %q", remedy, tc.wantRemedy)
			}
		})
	}

	t.Run("leaking reason is sanitized", func(t *testing.T) {
		tr := newFakeTransport()
		tr.forceStopResp = transport.AckResult{Ok: false, Reason: leakingReason}
		d := androiddriver.New(tr, process.NewFake(), androiddriver.Options{Serial: "S1"})

		_, msg, _ := diag.Describe(d.StopApp(context.Background(), "com.example.app"))
		if strings.Contains(msg, "127.0.0.1") || strings.Contains(msg, "/actions/") {
			t.Fatalf("message %q leaks transport detail", msg)
		}
	})
}
