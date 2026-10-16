package orchestra

import (
	"context"
	"testing"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/driver/mock"
)

const launchModeAppID = "com.example.app"

func launchCalls(d *mock.Driver) []driver.LaunchArgs {
	var out []driver.LaunchArgs
	for _, call := range d.Calls {
		if call.Method == "LaunchApp" {
			out = append(out, call.Args[1].(driver.LaunchArgs))
		}
	}
	return out
}

func newLaunchModeOrchestra() (*Orchestra, *mock.Driver) {
	d := mock.New()
	d.Apps = []driver.InstalledApp{{BundleID: launchModeAppID}}
	o, _ := newTestOrchestra(d)
	return o, d
}

func TestLaunchAppWithMode_MapsModeToNoReset(t *testing.T) {
	tests := []struct {
		mode LaunchMode
		want string
	}{
		{LaunchModeDefault, "nil"},
		{LaunchModeFresh, "false"},
		{LaunchModeAttach, "true"},
	}
	for _, tc := range tests {
		t.Run(string(tc.mode), func(t *testing.T) {
			o, d := newLaunchModeOrchestra()
			if err := o.LaunchAppWithMode(context.Background(), launchModeAppID, tc.mode); err != nil {
				t.Fatalf("LaunchAppWithMode: %v", err)
			}
			calls := launchCalls(d)
			if len(calls) != 1 {
				t.Fatalf("got %d driver launches, want 1", len(calls))
			}
			got := "nil"
			if calls[0].NoReset != nil {
				got = "false"
				if *calls[0].NoReset {
					got = "true"
				}
			}
			if got != tc.want {
				t.Fatalf("NoReset = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestLaunchAppWithMode_RepeatLaunchDedup(t *testing.T) {
	tests := []struct {
		mode      LaunchMode
		wantCalls int
	}{
		{LaunchModeDefault, 1},
		{LaunchModeAttach, 1},
		{LaunchModeFresh, 2},
	}
	for _, tc := range tests {
		t.Run(string(tc.mode), func(t *testing.T) {
			o, d := newLaunchModeOrchestra()
			for i := 0; i < 2; i++ {
				if err := o.LaunchAppWithMode(context.Background(), launchModeAppID, tc.mode); err != nil {
					t.Fatalf("launch %d: %v", i, err)
				}
			}
			if got := len(launchCalls(d)); got != tc.wantCalls {
				t.Fatalf("driver launches = %d, want %d", got, tc.wantCalls)
			}
		})
	}
}

func TestLaunchAppWithMode_FreshAfterDefaultRelaunches(t *testing.T) {
	o, d := newLaunchModeOrchestra()
	ctx := context.Background()
	if err := o.LaunchAppWithMode(ctx, launchModeAppID, LaunchModeDefault); err != nil {
		t.Fatal(err)
	}
	if err := o.LaunchAppWithMode(ctx, launchModeAppID, LaunchModeFresh); err != nil {
		t.Fatal(err)
	}
	if got := len(launchCalls(d)); got != 2 {
		t.Fatalf("driver launches = %d, want 2", got)
	}
}

func TestLaunchAppWithMode_UnknownModeRejectedBeforeDevice(t *testing.T) {
	o, d := newLaunchModeOrchestra()
	err := o.LaunchAppWithMode(context.Background(), launchModeAppID, LaunchMode("bogus"))
	if code, _, _ := diag.Describe(err); code != diag.CodeInvalidArgument {
		t.Fatalf("code = %q for an unknown mode, want %q (err %v)", code, diag.CodeInvalidArgument, err)
	}
	if len(d.Calls) != 0 {
		t.Fatalf("unknown mode reached the device: %v", d.Calls)
	}
}

func TestLaunchModeFromNoReset(t *testing.T) {
	yes, no := true, false
	if got := LaunchModeFromNoReset(nil); got != LaunchModeDefault {
		t.Fatalf("nil: %s", got)
	}
	if got := LaunchModeFromNoReset(&yes); got != LaunchModeAttach {
		t.Fatalf("true: %s", got)
	}
	if got := LaunchModeFromNoReset(&no); got != LaunchModeFresh {
		t.Fatalf("false: %s", got)
	}
}

func TestAppTermination_AllowsTheNextLaunchToRun(t *testing.T) {
	terminations := map[string]func(*Orchestra) error{
		"StopApp": func(o *Orchestra) error { return o.StopApp(context.Background(), launchModeAppID) },
		"KillApp": func(o *Orchestra) error { return o.KillApp(context.Background(), launchModeAppID) },
		"ClearAppData": func(o *Orchestra) error {
			return o.ClearAppData(context.Background(), launchModeAppID, driver.ClearAppDataOptions{})
		},
	}
	for name, terminate := range terminations {
		t.Run(name, func(t *testing.T) {
			o, d := newLaunchModeOrchestra()
			if err := o.LaunchAppWithMode(context.Background(), launchModeAppID, LaunchModeDefault); err != nil {
				t.Fatalf("first launch: %v", err)
			}
			if !o.IsLaunched(launchModeAppID) {
				t.Fatalf("the app must count as launched after the first launch")
			}
			if err := terminate(o); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if o.IsLaunched(launchModeAppID) {
				t.Fatalf("the app must not count as launched after %s", name)
			}
			if err := o.LaunchAppWithMode(context.Background(), launchModeAppID, LaunchModeDefault); err != nil {
				t.Fatalf("second launch: %v", err)
			}
			if got := len(launchCalls(d)); got != 2 {
				t.Fatalf("got %d driver launches, want 2", got)
			}
		})
	}
}
