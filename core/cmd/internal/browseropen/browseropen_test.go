package browseropen

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"dev.atomyx/internal/process"
)

type recordingRunner struct {
	name string
	args []string
	err  error
}

func (r *recordingRunner) Run(_ context.Context, name string, args ...string) (process.Result, error) {
	r.name, r.args = name, args
	return process.Result{}, r.err
}

func TestOpen_UsesThePlatformOpener(t *testing.T) {
	const url = "http://127.0.0.1:5000/#code"
	cases := map[string]struct {
		name string
		args []string
	}{
		"darwin":  {"open", []string{url}},
		"linux":   {"xdg-open", []string{url}},
		"windows": {"rundll32", []string{"url.dll,FileProtocolHandler", url}},
	}
	for goos, want := range cases {
		runner := &recordingRunner{}
		if err := New(runner, goos).Open(url); err != nil {
			t.Fatalf("%s: %v", goos, err)
		}
		if runner.name != want.name || !reflect.DeepEqual(runner.args, want.args) {
			t.Errorf("%s: ran %s %v, want %s %v", goos, runner.name, runner.args, want.name, want.args)
		}
	}
}

func TestOpen_RefusesAnUnknownPlatformANonHTTPURLAndAFailingOpener(t *testing.T) {
	runner := &recordingRunner{}
	if err := New(runner, "plan9").Open("http://127.0.0.1:1/"); err == nil {
		t.Error("an unknown platform opened a URL")
	}
	if err := New(runner, "darwin").Open("file:///etc/passwd"); err == nil || runner.name != "" {
		t.Errorf("a non-http URL was passed to the opener: %v, ran %q", err, runner.name)
	}
	failing := &recordingRunner{err: errors.New("boom")}
	if err := New(failing, "darwin").Open("http://127.0.0.1:1/"); !errors.Is(err, failing.err) {
		t.Errorf("error = %v, want the opener's error", err)
	}
}
