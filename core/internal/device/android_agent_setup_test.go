package device

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/process"
	"dev.atomyx/internal/version"
)

// setupAgentServer serves the given handlers as the agent's HTTP routes
// and returns an AndroidAgentSetup whose forward list points serial S1
// at that server.
func setupAgentServer(t *testing.T, routes map[string]http.HandlerFunc) (*AndroidAgentSetup, *process.Fake) {
	t.Helper()
	mux := http.NewServeMux()
	for path, handler := range routes {
		mux.HandleFunc(path, handler)
	}
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	runner := process.NewFake()
	runner.Stub(process.ExactMatch("adb", "forward", "--list"), process.Result{Stdout: fmt.Sprintf("S1 tcp:%s tcp:8765\n", parsed.Port())}, nil)
	return NewAndroidAgentSetup(AndroidAgentSetupDeps{Runner: runner}), runner
}

func writeJSON(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
}

func rejectWith(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

func TestAndroidAgentSetupAgentHealth(t *testing.T) {
	cases := []struct {
		name      string
		handler   http.HandlerFunc
		ready, ax bool
	}{
		{"ready", writeJSON(`{"ok":true,"accessibilityConnected":true}`), true, true},
		{"service not bound", writeJSON(`{"ok":true,"accessibilityConnected":false}`), true, false},
		{"error status counts as not ready", rejectWith(http.StatusServiceUnavailable, `{"ok":false,"reason":"starting"}`), false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setup, _ := setupAgentServer(t, map[string]http.HandlerFunc{"/health": tc.handler})
			ready, ax, err := setup.AgentHealth(context.Background(), "S1")
			if err != nil || ready != tc.ready || ax != tc.ax {
				t.Errorf("AgentHealth = %v, %v, %v, want %v, %v", ready, ax, err, tc.ready, tc.ax)
			}
		})
	}
}

func TestAndroidAgentSetupAgentHealthUnreachableIsAnError(t *testing.T) {
	runner := process.NewFake()
	runner.Stub(process.ExactMatch("adb", "forward", "--list"), process.Result{Stdout: "S1 tcp:1 tcp:8765\n"}, nil)
	setup := NewAndroidAgentSetup(AndroidAgentSetupDeps{Runner: runner})
	if _, _, err := setup.AgentHealth(context.Background(), "S1"); err == nil {
		t.Error("a closed port must be an error, not a not-ready answer")
	}
}

func TestAndroidAgentSetupAgentProtocol(t *testing.T) {
	current := fmt.Sprintf(`{"ok":true,"protocolVersion":%d}`, version.AgentProtocolVersion)
	cases := []struct {
		name    string
		handler http.HandlerFunc
		want    int
	}{
		{"current agent", writeJSON(current), version.AgentProtocolVersion},
		{"agent without the field", writeJSON(`{"ok":true}`), 0},
		{"agent that predates the route", rejectWith(http.StatusNotFound, `{"error":"no route"}`), 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setup, _ := setupAgentServer(t, map[string]http.HandlerFunc{"/ping": tc.handler})
			agent, host, err := setup.AgentProtocol(context.Background(), "S1")
			if err != nil || agent != tc.want || host != version.AgentProtocolVersion {
				t.Errorf("AgentProtocol = %d, %d, %v, want %d, %d", agent, host, err, tc.want, version.AgentProtocolVersion)
			}
		})
	}
	runner := process.NewFake()
	runner.Stub(process.ExactMatch("adb", "forward", "--list"), process.Result{Stdout: "S1 tcp:1 tcp:8765\n"}, nil)
	if _, _, err := NewAndroidAgentSetup(AndroidAgentSetupDeps{Runner: runner}).AgentProtocol(context.Background(), "S1"); err == nil {
		t.Error("an unreachable agent must be an error")
	}
}

func TestAndroidAgentSetupReadsInstallAndAccessibilityThroughAdb(t *testing.T) {
	runner := process.NewFake()
	runner.Stub(process.ExactMatch("adb", "-s", "S1", "shell", "pm", "list", "packages", "dev.atomyx.agent"), process.Result{Stdout: "package:dev.atomyx.agent\n"}, nil)
	runner.Stub(process.ExactMatch("adb", "-s", "S1", "shell", "settings", "get", "secure", "enabled_accessibility_services"), process.Result{Stdout: "com.a/.One\n"}, nil)
	setup := NewAndroidAgentSetup(AndroidAgentSetupDeps{Runner: runner})

	if installed, err := setup.AgentInstalled(context.Background(), "S1"); err != nil || !installed {
		t.Errorf("AgentInstalled = %v, %v", installed, err)
	}
	agent, others, err := setup.AccessibilityServices(context.Background(), "S1")
	if err != nil || agent || len(others) != 1 || others[0] != "com.a/.One" {
		t.Errorf("AccessibilityServices = %v, %v, %v", agent, others, err)
	}
}

func TestAndroidAgentSetupRepairsRunThroughTheLauncher(t *testing.T) {
	apk := filepath.Join(t.TempDir(), "app-debug.apk")
	if err := os.WriteFile(apk, []byte("apk"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := process.NewFake()
	runner.Stub(process.ExactMatch("adb", "forward", "--list"), process.Result{Stdout: "S1 tcp:1 tcp:8765\n"}, nil)
	setup := NewAndroidAgentSetup(AndroidAgentSetupDeps{Runner: runner, Clock: clock.NewFakeClock(0), APKPath: apk})

	if path, found := setup.ResolveAgentAPK(); path != apk || !found {
		t.Errorf("ResolveAgentAPK = %q, %v", path, found)
	}
	if err := setup.EnableAgentAccessibility(context.Background(), "S1"); err != nil {
		t.Fatalf("EnableAgentAccessibility() error = %v", err)
	}

	var commands []string
	for _, inv := range runner.Invocations() {
		commands = append(commands, strings.Join(inv.Args, " "))
	}
	joined := strings.Join(commands, "\n")
	for _, want := range []string{
		"-s S1 shell settings delete secure enabled_accessibility_services",
		"-s S1 shell settings put secure enabled_accessibility_services dev.atomyx.agent/dev.atomyx.agent.service.AtomyxAccessibilityService",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("adb commands missing %q:\n%s", want, joined)
		}
	}
}

func installCommands(runner *process.Fake) []string {
	var installs []string
	for _, inv := range runner.Invocations() {
		if len(inv.Args) > 2 && inv.Args[2] == "install" {
			installs = append(installs, strings.Join(inv.Args, " "))
		}
	}
	return installs
}

func TestAndroidAgentSetupInstallLeavesAHealthyAgentAloneButReinstallReplacesIt(t *testing.T) {
	apk := filepath.Join(t.TempDir(), "app-debug.apk")
	if err := os.WriteFile(apk, []byte("apk"), 0o600); err != nil {
		t.Fatal(err)
	}
	setup, runner := setupAgentServer(t, map[string]http.HandlerFunc{
		"/health": writeJSON(`{"ok":true,"accessibilityConnected":true}`),
	})
	setup.apkPath = apk
	setup.clock = clock.NewFakeClock(0)

	if err := setup.InstallAgent(context.Background(), "S1"); err != nil {
		t.Fatalf("InstallAgent() error = %v", err)
	}
	if got := installCommands(runner); len(got) != 0 {
		t.Fatalf("a healthy agent was reinstalled by InstallAgent: %v", got)
	}
	if err := setup.ReinstallAgent(context.Background(), "S1"); err != nil {
		t.Fatalf("ReinstallAgent() error = %v", err)
	}
	if got := installCommands(runner); len(got) != 1 || !strings.HasSuffix(got[0], apk) {
		t.Fatalf("install commands = %v, want one install of %s", got, apk)
	}
}
