package device

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/process"
)

// healthyAgentPort serves a ready agent and returns its host port.
func healthyAgentPort(t *testing.T) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/health", writeJSON(`{"ok":true,"accessibilityConnected":true}`))
	mux.HandleFunc("/ping", writeJSON(`{"ok":true,"protocolVersion":1}`))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	return parsed.Port()
}

func commandLines(runner *process.Fake) []string {
	var lines []string
	for _, inv := range runner.Invocations() {
		lines = append(lines, strings.Join(inv.Args, " "))
	}
	return lines
}

func removals(runner *process.Fake) []string {
	var found []string
	for _, line := range commandLines(runner) {
		if strings.Contains(line, "forward --remove") {
			found = append(found, line)
		}
	}
	return found
}

func TestAndroidAgentSetupRemovesTheForwardItCreated(t *testing.T) {
	port := healthyAgentPort(t)
	runner := process.NewFake()
	runner.Stub(process.ExactMatch("adb", "forward", "--list"), process.Result{}, nil)
	runner.Stub(process.ExactMatch("adb", "-s", "S1", "forward", "tcp:0", "tcp:8765"), process.Result{Stdout: port + "\n"}, nil)
	setup := NewAndroidAgentSetup(AndroidAgentSetupDeps{Runner: runner, Clock: clock.NewFakeClock(0)})

	if ready, _, err := setup.AgentHealth(context.Background(), "S1"); err != nil || !ready {
		t.Fatalf("AgentHealth = %v, %v", ready, err)
	}
	if _, _, err := setup.AgentProtocol(context.Background(), "S1"); err != nil {
		t.Fatalf("AgentProtocol error = %v", err)
	}
	want := "-s S1 forward --remove tcp:" + port
	if got := removals(runner); len(got) != 2 || got[0] != want || got[1] != want {
		t.Errorf("removals = %v, want the created forward removed after each call", got)
	}
}

func TestAndroidAgentSetupKeepsAForwardThatExistedBefore(t *testing.T) {
	setup, runner := setupAgentServer(t, map[string]http.HandlerFunc{
		"/health": writeJSON(`{"ok":true,"accessibilityConnected":true}`),
	})
	if _, _, err := setup.AgentHealth(context.Background(), "S1"); err != nil {
		t.Fatalf("AgentHealth error = %v", err)
	}
	if got := removals(runner); len(got) != 0 {
		t.Errorf("a forward that existed before was removed: %v", got)
	}
}

func TestAndroidAgentSetupRemovesTheForwardEvenWhenTheCallFails(t *testing.T) {
	runner := process.NewFake()
	runner.Stub(process.ExactMatch("adb", "forward", "--list"), process.Result{}, nil)
	runner.Stub(process.ExactMatch("adb", "-s", "S1", "forward", "tcp:0", "tcp:8765"), process.Result{Stdout: "1\n"}, nil)
	setup := NewAndroidAgentSetup(AndroidAgentSetupDeps{Runner: runner, Clock: clock.NewFakeClock(0)})

	if _, _, err := setup.AgentHealth(context.Background(), "S1"); err == nil {
		t.Fatal("a closed port must be an error")
	}
	if got := removals(runner); len(got) != 1 {
		t.Errorf("removals = %v, want the created forward removed", got)
	}
}

func TestAndroidAgentSetupErrorsCarryACodeAndNoPackagePrefix(t *testing.T) {
	failing := process.NewFake()
	failing.DefaultErr = errors.New("androiddriver: adb exploded")
	setup := NewAndroidAgentSetup(AndroidAgentSetupDeps{Runner: failing, Clock: clock.NewFakeClock(0), APKPath: t.TempDir()})
	ctx := context.Background()
	calls := map[string]func() error{
		"AgentInstalled": func() error { _, err := setup.AgentInstalled(ctx, "S1"); return err },
		"InstallAgent":   func() error { return setup.InstallAgent(ctx, "S1") },
		"ReinstallAgent": func() error { return setup.ReinstallAgent(ctx, "S1") },
		"StartAgent":     func() error { return setup.StartAgent(ctx, "S1") },
		"AccessibilityServices": func() error {
			_, _, err := setup.AccessibilityServices(ctx, "S1")
			return err
		},
		"EnableAgentAccessibility": func() error { return setup.EnableAgentAccessibility(ctx, "S1") },
		"AgentHealth":              func() error { _, _, err := setup.AgentHealth(ctx, "S1"); return err },
		"AgentProtocol":            func() error { _, _, err := setup.AgentProtocol(ctx, "S1"); return err },
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			err := call()
			if err == nil {
				t.Fatal("expected an error")
			}
			code, message, remedy := diag.Describe(err)
			if code == diag.CodeUnknown || remedy == "" {
				t.Errorf("code = %q, remedy = %q, want a coded error with a remedy", code, remedy)
			}
			for _, prefix := range []string{"androiddriver:", "agentlauncher:", "transport:", "process:"} {
				if strings.Contains(message, prefix) {
					t.Errorf("message %q contains the package prefix %q", message, prefix)
				}
			}
		})
	}
}

func TestAndroidAgentSetupLauncherFailuresKeepTheirMeaning(t *testing.T) {
	runner := process.NewFake()
	port := healthyAgentPort(t)
	runner.Stub(process.ExactMatch("adb", "forward", "--list"), process.Result{Stdout: fmt.Sprintf("S1 tcp:%s tcp:8765\n", port)}, nil)
	setup := NewAndroidAgentSetup(AndroidAgentSetupDeps{Runner: runner, Clock: clock.NewFakeClock(0), APKPath: t.TempDir() + "/missing.apk"})

	code, _, remedy := diag.Describe(setup.ReinstallAgent(context.Background(), "S1"))
	if code != diag.CodeAppInstallFailed || !strings.Contains(remedy, "ATOMYX_ANDROID_APK") {
		t.Errorf("a missing APK: code = %q, remedy = %q", code, remedy)
	}
}

func TestAndroidAgentSetupReadsSendNoInstallSettingsWriteOrActivityManagerCommand(t *testing.T) {
	setup, runner := setupAgentServer(t, map[string]http.HandlerFunc{
		"/health": writeJSON(`{"ok":true,"accessibilityConnected":true}`),
		"/ping":   writeJSON(`{"ok":true,"protocolVersion":1}`),
	})
	ctx := context.Background()
	if _, err := setup.AgentInstalled(ctx, "S1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := setup.AccessibilityServices(ctx, "S1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := setup.AgentHealth(ctx, "S1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := setup.AgentProtocol(ctx, "S1"); err != nil {
		t.Fatal(err)
	}
	if len(runner.Invocations()) == 0 {
		t.Fatal("the reads ran no command, so the assertion below proves nothing")
	}
	for _, inv := range runner.Invocations() {
		args := inv.Args
		for i, arg := range args {
			mutating := arg == "install" ||
				arg == "am" ||
				(arg == "settings" && i+1 < len(args) && (args[i+1] == "put" || args[i+1] == "delete"))
			if mutating {
				t.Errorf("a read sent a changing command: adb %s", strings.Join(args, " "))
			}
		}
	}
}

func TestAndroidAgentSetupMalformedAgentAnswerIsACodedError(t *testing.T) {
	setup, _ := setupAgentServer(t, map[string]http.HandlerFunc{
		"/health": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("not json")) },
		"/ping":   func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("not json")) },
	})
	ctx := context.Background()
	_, _, healthErr := setup.AgentHealth(ctx, "S1")
	_, _, protocolErr := setup.AgentProtocol(ctx, "S1")
	for name, err := range map[string]error{"health": healthErr, "protocol": protocolErr} {
		if err == nil {
			t.Errorf("%s: a malformed answer must be an error", name)
			continue
		}
		code, message, remedy := diag.Describe(err)
		if code == diag.CodeUnknown || remedy == "" || strings.Contains(message, "127.0.0.1") {
			t.Errorf("%s: code = %q, message = %q, remedy = %q", name, code, message, remedy)
		}
	}
}
