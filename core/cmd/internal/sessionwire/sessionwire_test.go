package sessionwire

import (
	"testing"

	"dev.atomyx/internal/config"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/session"
)

func env(values map[string]string) func(string) string {
	return func(k string) string { return values[k] }
}

func ptr(n int64) *int64 { return &n }

func TestResolveTimeouts_DefaultsApplyWithNoConfigAndNoEnvironment(t *testing.T) {
	got := ResolveTimeouts(nil, env(nil), logging.NoopLogger{})
	if got != session.DefaultTimeouts() {
		t.Fatalf("got %+v, want the defaults %+v", got, session.DefaultTimeouts())
	}
}

func TestResolveTimeouts_ConfigOverridesDefaultsAndEnvironmentOverridesConfig(t *testing.T) {
	cfg := &config.AtomyxConfig{Session: &config.Session{
		McpIdleTimeoutMs: ptr(1000), OpenRunIdleTimeoutMs: ptr(2000), DeviceAcquireTimeoutMs: ptr(3000),
	}}

	fromConfigOnly := ResolveTimeouts(cfg, env(nil), logging.NoopLogger{})
	if fromConfigOnly != (session.Timeouts{McpIdleMs: 1000, OpenRunIdleMs: 2000, DeviceAcquireMs: 3000}) {
		t.Fatalf("config only = %+v", fromConfigOnly)
	}

	withEnv := ResolveTimeouts(cfg, env(map[string]string{McpIdleEnvVar: "20000", DeviceAcquireEnvVar: "90000"}), logging.NoopLogger{})
	if withEnv != (session.Timeouts{McpIdleMs: 20000, OpenRunIdleMs: 2000, DeviceAcquireMs: 90000}) {
		t.Fatalf("with environment = %+v, want the environment to beat config and leave the open-run timeout alone", withEnv)
	}
}

func TestResolveTimeouts_AnInvalidEnvironmentValueIsIgnored(t *testing.T) {
	for _, bad := range []string{"abc", "0", "-5", "1.5"} {
		got := ResolveTimeouts(nil, env(map[string]string{McpIdleEnvVar: bad}), logging.NoopLogger{})
		if got.McpIdleMs != session.DefaultMcpIdleTimeoutMs {
			t.Fatalf("%q gave %d, want the default", bad, got.McpIdleMs)
		}
	}
}
