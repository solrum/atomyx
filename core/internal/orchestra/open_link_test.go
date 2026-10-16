package orchestra

import (
	"context"
	"testing"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/driver/mock"
)

func openLinkCalls(d *mock.Driver) [][]any {
	var out [][]any
	for _, call := range d.Calls {
		if call.Method == "OpenLink" {
			out = append(out, call.Args)
		}
	}
	return out
}

func TestOpenLink_PassesTheLinkAndAppToTheDriver(t *testing.T) {
	d := mock.New()
	o, _ := newTestOrchestra(d)

	if err := o.OpenLink(context.Background(), "myapp://orders/42", "com.example.app"); err != nil {
		t.Fatalf("OpenLink: %v", err)
	}
	calls := openLinkCalls(d)
	if len(calls) != 1 || calls[0][0] != "myapp://orders/42" || calls[0][1] != "com.example.app" {
		t.Fatalf("driver OpenLink calls = %v, want one for the link and app", calls)
	}
}

func TestOpenLink_RejectsBadInputBeforeTheDriver(t *testing.T) {
	tests := []struct {
		name     string
		link     string
		appID    string
		wantCode string
	}{
		{"no scheme", "orders/42", "", diag.CodeInvalidArgument},
		{"empty link", "", "", diag.CodeInvalidArgument},
		{"bad app id", "myapp://home", "bad id!", diag.CodeInvalidAppID},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := mock.New()
			o, _ := newTestOrchestra(d)
			err := o.OpenLink(context.Background(), tc.link, tc.appID)
			if code, _, _ := diag.Describe(err); code != tc.wantCode {
				t.Fatalf("OpenLink code = %q (err %v), want %q", code, err, tc.wantCode)
			}
			if len(openLinkCalls(d)) != 0 {
				t.Fatal("driver OpenLink was called")
			}
		})
	}
}

func TestOpenLink_ReportsUnsupportedWhenTheCapabilityIsOff(t *testing.T) {
	d := mock.New()
	d.SetCapabilities(driver.Capabilities{CanOpenLink: false})
	o, _ := newTestOrchestra(d)

	err := o.OpenLink(context.Background(), "myapp://home", "")
	if code, _, _ := diag.Describe(err); code != diag.CodeLinkUnsupported {
		t.Fatalf("OpenLink code = %q (err %v), want %q", code, err, diag.CodeLinkUnsupported)
	}
	if len(openLinkCalls(d)) != 0 {
		t.Fatal("driver OpenLink was called")
	}
}

func TestOpenLink_InAnAppMarksItLaunchedSoADefaultLaunchIsSkipped(t *testing.T) {
	o, d := newLaunchModeOrchestra()

	if err := o.OpenLink(context.Background(), "myapp://home", launchModeAppID); err != nil {
		t.Fatalf("OpenLink: %v", err)
	}
	if err := o.LaunchAppWithMode(context.Background(), launchModeAppID, LaunchModeDefault); err != nil {
		t.Fatalf("LaunchAppWithMode: %v", err)
	}
	if got := launchCalls(d); len(got) != 0 {
		t.Fatalf("driver launches = %v, want none after a link opened the app", got)
	}
}

func TestLaunchAppWithOptions_LinkOpensTheLinkInsteadOfLaunching(t *testing.T) {
	o, d := newLaunchModeOrchestra()

	err := o.LaunchAppWithOptions(context.Background(), launchModeAppID, LaunchOptions{Link: "myapp://orders/42"})
	if err != nil {
		t.Fatalf("LaunchAppWithOptions: %v", err)
	}
	if got := launchCalls(d); len(got) != 0 {
		t.Fatalf("driver launches = %v, want none", got)
	}
	calls := openLinkCalls(d)
	if len(calls) != 1 || calls[0][0] != "myapp://orders/42" || calls[0][1] != launchModeAppID {
		t.Fatalf("driver OpenLink calls = %v, want one for the link in the app", calls)
	}
}

func TestLaunchAppWithOptions_LinkRejectsModeAndArguments(t *testing.T) {
	tests := []struct {
		name string
		opts LaunchOptions
	}{
		{"fresh mode", LaunchOptions{Mode: LaunchModeFresh, Link: "myapp://home"}},
		{"attach mode", LaunchOptions{Mode: LaunchModeAttach, Link: "myapp://home"}},
		{"arguments", LaunchOptions{Arguments: map[string]string{"a": "b"}, Link: "myapp://home"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o, d := newLaunchModeOrchestra()
			err := o.LaunchAppWithOptions(context.Background(), launchModeAppID, tc.opts)
			if code, _, _ := diag.Describe(err); code != diag.CodeInvalidArgument {
				t.Fatalf("code = %q (err %v), want %q", code, err, diag.CodeInvalidArgument)
			}
			if len(openLinkCalls(d)) != 0 || len(launchCalls(d)) != 0 {
				t.Fatal("the driver was called")
			}
		})
	}
}
