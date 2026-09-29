package orchestra

import (
	"context"
	"errors"
	"testing"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/driver/mock"
)

func TestValidateAppID_AcceptsWellFormedIDs(t *testing.T) {
	cases := []string{
		"com.example.app",
		"com.example.app123-beta",
		"dev.atomyx.driver.host",
	}
	for _, appID := range cases {
		if err := validateAppID(appID); err != nil {
			t.Errorf("validateAppID(%q) = %v, want nil", appID, err)
		}
	}
}

func TestValidateAppID_RejectsDisallowedCharacters(t *testing.T) {
	cases := map[string]string{
		"space":     "com.example app",
		"semicolon": "com.example.app; rm -rf /",
		"backtick":  "com.example.app`whoami`",
		"dollar":    "com.example.app$(whoami)",
		"slash":     "com.example/app",
		"quote":     "com.example.app\"",
	}
	for name, appID := range cases {
		t.Run(name, func(t *testing.T) {
			err := validateAppID(appID)
			assertInvalidAppID(t, err)
		})
	}
}

func TestValidateAppID_RejectsEmptyAndOverlong(t *testing.T) {
	if err := validateAppID(""); err == nil {
		t.Fatal("validateAppID(\"\") = nil, want an error")
	} else {
		assertInvalidAppID(t, err)
	}

	overlong := make([]byte, maxAppIDLength+1)
	for i := range overlong {
		overlong[i] = 'a'
	}
	if err := validateAppID(string(overlong)); err == nil {
		t.Fatal("validateAppID(overlong) = nil, want an error")
	} else {
		assertInvalidAppID(t, err)
	}
}

// TestOrchestra_AppIDMethods_RejectInvalidAppIDWithoutDeviceCall covers
// every Orchestra method that carries an app id to a device call.
// Each one must reject a shell-metacharacter-bearing id with
// CodeInvalidAppID before the mock driver records any call, proving
// the bad id never reaches the adapter.
func TestOrchestra_AppIDMethods_RejectInvalidAppIDWithoutDeviceCall(t *testing.T) {
	const maliciousAppID = "com.example.app; rm -rf /"

	methods := map[string]func(o *Orchestra) error{
		"LaunchApp": func(o *Orchestra) error {
			return o.LaunchApp(context.Background(), maliciousAppID, driver.LaunchArgs{})
		},
		"StopApp": func(o *Orchestra) error {
			return o.StopApp(context.Background(), maliciousAppID)
		},
		"KillApp": func(o *Orchestra) error {
			return o.KillApp(context.Background(), maliciousAppID)
		},
		"UninstallApp": func(o *Orchestra) error {
			return o.UninstallApp(context.Background(), maliciousAppID)
		},
		"ClearAppData": func(o *Orchestra) error {
			return o.ClearAppData(context.Background(), maliciousAppID, driver.ClearAppDataOptions{})
		},
		"IsAppInstalled": func(o *Orchestra) error {
			_, err := o.IsAppInstalled(context.Background(), maliciousAppID)
			return err
		},
	}

	for name, invoke := range methods {
		t.Run(name, func(t *testing.T) {
			d := mock.New()
			o, _ := newTestOrchestra(d)

			err := invoke(o)
			assertInvalidAppID(t, err)

			if len(d.Calls) != 0 {
				t.Fatalf("expected no driver call, got %v", d.Calls)
			}
		})
	}
}

func assertInvalidAppID(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	var coded diag.Coded
	if !errors.As(err, &coded) {
		t.Fatalf("expected a diag.Coded error, got %v", err)
	}
	if coded.Code() != diag.CodeInvalidAppID {
		t.Fatalf("got code %q, want %q", coded.Code(), diag.CodeInvalidAppID)
	}
}
