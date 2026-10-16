package androiddriver_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"dev.atomyx/internal/androiddriver"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/process"
)

func openLinkInvocation(args ...string) process.Invocation {
	full := append([]string{"-s", "S1", "shell", "am", "start", "-a", "android.intent.action.VIEW", "-d"}, args...)
	return process.Invocation{Name: "adb", Args: full}
}

func newOpenLinkDriver(runner *process.Fake) *androiddriver.AndroidDriver {
	return androiddriver.New(newFakeTransport(), runner, androiddriver.Options{Serial: "S1"})
}

func TestAndroidDriver_OpenLink_StartsAViewIntentForTheQuotedLink(t *testing.T) {
	cases := []struct {
		name string
		link string
		want string
	}{
		{name: "plain", link: "atomyxdemo://open/focus", want: `'atomyxdemo://open/focus'`},
		{name: "ampersand and query", link: "https://example.com/a?x=1&y=2", want: `'https://example.com/a?x=1&y=2'`},
		{name: "spaces", link: "myapp://search/two words", want: `'myapp://search/two words'`},
		{name: "single quote", link: "myapp://say/it's", want: `'myapp://say/it'\''s'`},
		{name: "double quote and dollar", link: `myapp://q/"$HOME"`, want: `'myapp://q/"$HOME"'`},
		{name: "semicolon and backtick", link: "myapp://x;rm -rf /`id`", want: "'myapp://x;rm -rf /`id`'"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runner := process.NewFake()
			d := newOpenLinkDriver(runner)

			if err := d.OpenLink(context.Background(), tc.link, driver.OpenLinkOptions{}); err != nil {
				t.Fatalf("OpenLink() error = %v", err)
			}
			got := runner.Invocations()
			if len(got) != 1 || !reflect.DeepEqual(got[0], openLinkInvocation(tc.want)) {
				t.Fatalf("OpenLink() invocations = %+v, want one %+v", got, openLinkInvocation(tc.want))
			}
		})
	}
}

func TestAndroidDriver_OpenLink_RestrictsTheIntentToTheNamedApp(t *testing.T) {
	runner := process.NewFake()
	d := newOpenLinkDriver(runner)

	if err := d.OpenLink(context.Background(), "atomyxdemo://open/focus", driver.OpenLinkOptions{AppID: "dev.atomyx.demo"}); err != nil {
		t.Fatalf("OpenLink() error = %v", err)
	}
	want := openLinkInvocation(`'atomyxdemo://open/focus'`, "-p", `'dev.atomyx.demo'`)
	if got := runner.Invocations(); len(got) != 1 || !reflect.DeepEqual(got[0], want) {
		t.Fatalf("OpenLink() invocations = %+v, want one %+v", got, want)
	}
}

func TestAndroidDriver_OpenLink_RejectsBadInputBeforeTheShell(t *testing.T) {
	cases := []struct {
		name string
		link string
		opts driver.OpenLinkOptions
	}{
		{name: "empty link", link: ""},
		{name: "no scheme", link: "example.com/path"},
		{name: "control character", link: "myapp://a\nb"},
		{name: "bad package", link: "myapp://home", opts: driver.OpenLinkOptions{AppID: "com.x; reboot"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runner := process.NewFake()
			d := newOpenLinkDriver(runner)

			err := d.OpenLink(context.Background(), tc.link, tc.opts)
			if code, _, _ := diag.Describe(err); code != diag.CodeInvalidArgument {
				t.Fatalf("OpenLink() error code = %q (err %v), want %q", code, err, diag.CodeInvalidArgument)
			}
			if got := runner.Invocations(); len(got) != 0 {
				t.Fatalf("OpenLink() ran %+v, want no shell call", got)
			}
		})
	}
}

func TestAndroidDriver_OpenLink_ReportsALinkNoAppHandles(t *testing.T) {
	runner := process.NewFake()
	runner.Default = process.Result{
		Stdout: "Starting: Intent { act=android.intent.action.VIEW dat=nobody://secret-token }\n" +
			"Error: Activity not started, unable to resolve Intent { act=android.intent.action.VIEW dat=nobody://secret-token }\n",
	}
	d := newOpenLinkDriver(runner)

	err := d.OpenLink(context.Background(), "nobody://secret-token", driver.OpenLinkOptions{})
	code, message, _ := diag.Describe(err)
	if code != diag.CodeLinkNotOpened {
		t.Fatalf("OpenLink() code = %q (err %v), want %q", code, err, diag.CodeLinkNotOpened)
	}
	if strings.Contains(message, "secret-token") {
		t.Fatalf("OpenLink() message %q leaks the link", message)
	}
}

func TestAndroidDriver_OpenLink_TreatsAWarningAsSuccess(t *testing.T) {
	runner := process.NewFake()
	runner.Default = process.Result{
		Stdout: "Starting: Intent { act=android.intent.action.VIEW dat=atomyxdemo://open/focus }\n" +
			"Warning: Activity not started, its current task has been brought to the front\n",
	}
	d := newOpenLinkDriver(runner)

	if err := d.OpenLink(context.Background(), "atomyxdemo://open/focus", driver.OpenLinkOptions{}); err != nil {
		t.Fatalf("OpenLink() error = %v, want nil", err)
	}
}

func TestAndroidDriver_OpenLink_ShellFailureIsLinkNotOpened(t *testing.T) {
	runner := process.NewFake()
	runner.Default = process.Result{Stderr: "Error: Permission denied\n", ExitCode: 1}
	runner.DefaultErr = errors.New("exit status 1")
	d := newOpenLinkDriver(runner)

	err := d.OpenLink(context.Background(), "atomyxdemo://open/focus", driver.OpenLinkOptions{})
	if code, _, _ := diag.Describe(err); code != diag.CodeLinkNotOpened {
		t.Fatalf("OpenLink() code = %q (err %v), want %q", code, err, diag.CodeLinkNotOpened)
	}
}

func TestAndroidDriver_OpenLink_UnreachableDeviceIsReportedAsUnreachable(t *testing.T) {
	runner := process.NewFake()
	runner.Default = process.Result{Stderr: "error: device offline\n", ExitCode: 1}
	runner.DefaultErr = errors.New("exit status 1")
	d := newOpenLinkDriver(runner)

	err := d.OpenLink(context.Background(), "atomyxdemo://open/focus", driver.OpenLinkOptions{})
	if code, _, _ := diag.Describe(err); code != diag.CodeAgentUnreachable {
		t.Fatalf("OpenLink() code = %q (err %v), want %q", code, err, diag.CodeAgentUnreachable)
	}
}

func TestAndroidDriver_OpenLink_ReportsCanOpenLink(t *testing.T) {
	d := newOpenLinkDriver(process.NewFake())
	if !d.Capabilities().CanOpenLink {
		t.Fatal("Capabilities().CanOpenLink = false, want true")
	}
}
