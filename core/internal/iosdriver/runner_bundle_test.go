package iosdriver

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"dev.atomyx/internal/process"
)

const (
	testProductPath = "/build/Debug-iphonesimulator/AtomyxDriverAgent-Runner.app"
	testPlistPath   = testProductPath + "/Info.plist"
)

func plutilMatcher() process.Matcher {
	return process.ExactMatch("plutil", "-convert", "json", "-o", "-", testPlistPath)
}

func TestResolveRunnerBundleID_ReadsTheIdFromTheProductPlist(t *testing.T) {
	runner := process.NewFake()
	runner.Stub(plutilMatcher(), process.Result{Stdout: `{"CFBundleIdentifier":"com.acme.agent.xctrunner","CFBundleName":"x"}`}, nil)

	got := ResolveRunnerBundleID(context.Background(), runner, testProductPath, DefaultRunnerBundleID)

	if got != "com.acme.agent.xctrunner" {
		t.Fatalf("ResolveRunnerBundleID() = %q, want com.acme.agent.xctrunner", got)
	}
}

func TestResolveRunnerBundleID_AppendsTheRunnerSuffixWhenThePlistOmitsIt(t *testing.T) {
	runner := process.NewFake()
	runner.Stub(plutilMatcher(), process.Result{Stdout: `{"CFBundleIdentifier":"com.acme.agent"}`}, nil)

	got := ResolveRunnerBundleID(context.Background(), runner, testProductPath, DefaultRunnerBundleID)

	if got != "com.acme.agent.xctrunner" {
		t.Fatalf("ResolveRunnerBundleID() = %q, want com.acme.agent.xctrunner", got)
	}
}

func TestResolveRunnerBundleID_FallsBackWhenTheIdCannotBeRead(t *testing.T) {
	const fallback = "fallback.xctrunner"
	cases := map[string]func(*process.Fake){
		"missing plist": func(r *process.Fake) { r.Stub(plutilMatcher(), process.Result{}, errors.New("no such file")) },
		"not json":      func(r *process.Fake) { r.Stub(plutilMatcher(), process.Result{Stdout: "garbage"}, nil) },
		"no id key":     func(r *process.Fake) { r.Stub(plutilMatcher(), process.Result{Stdout: `{"CFBundleName":"x"}`}, nil) },
		"blank id": func(r *process.Fake) {
			r.Stub(plutilMatcher(), process.Result{Stdout: `{"CFBundleIdentifier":" "}`}, nil)
		},
		"non string id": func(r *process.Fake) {
			r.Stub(plutilMatcher(), process.Result{Stdout: `{"CFBundleIdentifier":7}`}, nil)
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			runner := process.NewFake()
			setup(runner)
			if got := ResolveRunnerBundleID(context.Background(), runner, testProductPath, fallback); got != fallback {
				t.Fatalf("ResolveRunnerBundleID() = %q, want %q", got, fallback)
			}
		})
	}
}

func TestResolveRunnerBundleID_AnEmptyProductPathReturnsTheFallbackWithoutRunningAnything(t *testing.T) {
	runner := process.NewFake()

	got := ResolveRunnerBundleID(context.Background(), runner, "", DefaultRunnerBundleID)

	if got != DefaultRunnerBundleID {
		t.Fatalf("ResolveRunnerBundleID() = %q, want the default", got)
	}
	if calls := runner.Invocations(); len(calls) != 0 {
		t.Fatalf("runner calls = %v, want none", calls)
	}
}

func TestParseRunnerBundleID_HandlesAnXMLPlistConvertedToJSON(t *testing.T) {
	// plutil emits the same JSON for an XML and a binary plist, so the
	// parser sees only the converted form.
	data := []byte(`{"CFBundleExecutable":"AtomyxDriverAgent-Runner","CFBundleIdentifier":"dev.atomyx.driver.host.xctrunner","CFBundlePackageType":"APPL"}`)
	got, ok := parseRunnerBundleID(data)
	if !ok || got != DefaultRunnerBundleID {
		t.Fatalf("parseRunnerBundleID() = %q, %v, want the default id", got, ok)
	}
}

func requirePlutil(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("plutil"); err != nil {
		t.Skip("plutil is not available on this host")
	}
}

func TestResolveRunnerBundleID_ReadsAnXMLPlistFixture(t *testing.T) {
	requirePlutil(t)

	got := ResolveRunnerBundleID(context.Background(), process.NewExec(), "testdata/custom-runner.app", DefaultRunnerBundleID)

	if got != "com.acme.agent.xctrunner" {
		t.Fatalf("ResolveRunnerBundleID() = %q, want com.acme.agent.xctrunner", got)
	}
}

func TestResolveRunnerBundleID_ReadsABinaryPlist(t *testing.T) {
	requirePlutil(t)
	product := filepath.Join(t.TempDir(), "Runner.app")
	if err := os.Mkdir(product, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(product, "Info.plist")
	if out, err := exec.CommandContext(t.Context(), "plutil", "-convert", "binary1", "-o", target, "testdata/custom-runner.app/Info.plist").CombinedOutput(); err != nil {
		t.Fatalf("convert fixture: %v: %s", err, out)
	}

	got := ResolveRunnerBundleID(context.Background(), process.NewExec(), product, DefaultRunnerBundleID)

	if got != "com.acme.agent.xctrunner" {
		t.Fatalf("ResolveRunnerBundleID() = %q, want com.acme.agent.xctrunner", got)
	}
}

func TestResolveRunnerBundleID_AMissingProductFallsBack(t *testing.T) {
	requirePlutil(t)

	got := ResolveRunnerBundleID(context.Background(), process.NewExec(), filepath.Join(t.TempDir(), "absent.app"), DefaultRunnerBundleID)

	if got != DefaultRunnerBundleID {
		t.Fatalf("ResolveRunnerBundleID() = %q, want the default", got)
	}
}
