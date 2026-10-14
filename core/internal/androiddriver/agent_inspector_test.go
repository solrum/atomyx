package androiddriver

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"dev.atomyx/internal/process"
	"dev.atomyx/internal/transport"
)

func TestAgentInspectorInstalled(t *testing.T) {
	list := process.ExactMatch("adb", "-s", "s1", "shell", "pm", "list", "packages", "dev.atomyx.agent")
	cases := []struct {
		name   string
		stdout string
		err    error
		want   bool
	}{
		{"installed", "package:dev.atomyx.agent\n", nil, true},
		{"absent", "", nil, false},
		{"only a longer id", "package:dev.atomyx.agent.other\n", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := process.NewFake()
			fake.Stub(list, process.Result{Stdout: tc.stdout}, tc.err)
			got, err := NewAgentInspector(fake, "s1").Installed(context.Background())
			if err != nil || got != tc.want {
				t.Errorf("Installed = %v, %v, want %v", got, err, tc.want)
			}
		})
	}
	fake := process.NewFake()
	fake.Stub(list, process.Result{}, errors.New("device offline"))
	if _, err := NewAgentInspector(fake, "s1").Installed(context.Background()); err == nil {
		t.Error("an adb failure must surface")
	}
}

func TestAgentInspectorAccessibilityServices(t *testing.T) {
	get := process.ExactMatch("adb", "-s", "s1", "shell", "settings", "get", "secure", "enabled_accessibility_services")
	full := "dev.atomyx.agent/dev.atomyx.agent.service.AtomyxAccessibilityService"
	cases := []struct {
		name       string
		stdout     string
		wantAgent  bool
		wantOthers []string
	}{
		{"unset", "null\n", false, nil},
		{"empty", "\n", false, nil},
		{"agent only", full + "\n", true, nil},
		{"agent among others", "com.a/.One:" + full + ":com.b/com.b.Two\n", true, []string{"com.a/.One", "com.b/com.b.Two"}},
		{"relative class spelling", "dev.atomyx.agent/.service.AtomyxAccessibilityService\n", true, nil},
		{"another service only", "com.a/.One\n", false, []string{"com.a/.One"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := process.NewFake()
			fake.Stub(get, process.Result{Stdout: tc.stdout}, nil)
			agent, others, err := NewAgentInspector(fake, "s1").AccessibilityServices(context.Background())
			if err != nil || agent != tc.wantAgent || !reflect.DeepEqual(others, tc.wantOthers) {
				t.Errorf("got %v, %v, %v, want %v, %v", agent, others, err, tc.wantAgent, tc.wantOthers)
			}
		})
	}
	fake := process.NewFake()
	fake.Stub(get, process.Result{}, errors.New("no shell"))
	if _, _, err := NewAgentInspector(fake, "s1").AccessibilityServices(context.Background()); err == nil {
		t.Error("an adb failure must surface")
	}
}

func TestPingedProtocolVersion(t *testing.T) {
	if got, known := PingedProtocolVersion(transport.PingResponse{ProtocolVersion: 3}, nil); got != 3 || !known {
		t.Errorf("answered: %d, %v", got, known)
	}
	if got, known := PingedProtocolVersion(transport.PingResponse{}, &transport.AgentError{}); got != 0 || !known {
		t.Errorf("agent error counts as version 0: %d, %v", got, known)
	}
	if _, known := PingedProtocolVersion(transport.PingResponse{}, errors.New("connection refused")); known {
		t.Error("an unreachable route shows no version")
	}
}
