package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/driver"
	mockdriver "dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/session/sessiontest"
)

// agentFactory builds a connected mock driver that reports agent.
type agentFactory struct {
	agent driver.AgentInfo
}

func (f agentFactory) WithDriverWrap(wrap device.DriverWrap) device.Factory {
	return sessiontest.Wrapping(f).WithDriverWrap(wrap)
}

func (f agentFactory) Build(ctx context.Context, _ device.Descriptor) (device.Built, error) {
	drv := mockdriver.New()
	drv.Agent = f.agent
	if err := drv.Connect(ctx); err != nil {
		return device.Built{}, err
	}
	return device.Built{
		Driver:    drv,
		Orchestra: orchestra.New(orchestra.OrchestraDeps{Driver: drv, Clock: orchestra.NewFakeClock(0)}),
	}, nil
}

func agentDeps(stdout, stderr *bytes.Buffer, agent driver.AgentInfo) Deps {
	deps := mockDeps(stdout, stderr)
	deps.Factory = agentFactory{agent: agent}
	return deps
}

func TestRunRecordsTheAgentVersionsInTheHostOfTheMeta(t *testing.T) {
	tests := []struct {
		name  string
		agent driver.AgentInfo
		want  runfolder.Host
	}{
		{"reported", driver.AgentInfo{ProtocolVersion: 3, AgentVersion: "1.2.3"}, runfolder.Host{ProtocolVersion: 3, AgentVersion: "1.2.3"}},
		{"unreported", driver.AgentInfo{}, runfolder.Host{}},
	}
	for _, tc := range tests {
		t.Run("script "+tc.name, func(t *testing.T) {
			root := t.TempDir()
			scriptPath := writeScript(t, "flow.yml", "- screenshot: home\n")
			var stdout, stderr bytes.Buffer
			code := RunScript(context.Background(), agentDeps(&stdout, &stderr, tc.agent), map[string]FlagValue{
				"--file": scriptPath, "--report-dir": root,
			})
			if code != 0 {
				t.Fatalf("exit code %d, stderr: %s", code, stderr.String())
			}
			assertAgentHost(t, readMeta(t, singleRunDir(t, root)), tc.want)
		})
		t.Run("suite "+tc.name, func(t *testing.T) {
			root := t.TempDir()
			dir := t.TempDir()
			t.Chdir(dir)
			if err := os.WriteFile(filepath.Join(dir, "pass.yml"), []byte("- screenshot: home\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			code := RunSuite(context.Background(), agentDeps(&stdout, &stderr, tc.agent), map[string]FlagValue{
				"--glob": []string{"*.yml"}, "--report-dir": root,
			}, nil)
			if code != 0 {
				t.Fatalf("exit code %d, stderr: %s", code, stderr.String())
			}
			assertAgentHost(t, readMeta(t, singleRunDir(t, root)), tc.want)
		})
	}
}

// assertAgentHost checks the agent fields of the host and that the host
// still carries the atomyx version.
func assertAgentHost(t *testing.T, meta runfolder.Meta, want runfolder.Host) {
	t.Helper()
	if meta.Host == nil {
		t.Fatal("meta has no host")
	}
	if meta.Host.ProtocolVersion != want.ProtocolVersion || meta.Host.AgentVersion != want.AgentVersion {
		t.Errorf("host agent = %d %q, want %d %q", meta.Host.ProtocolVersion, meta.Host.AgentVersion, want.ProtocolVersion, want.AgentVersion)
	}
	if meta.Host.AtomyxVersion == "" {
		t.Error("host lost its atomyx version")
	}
}
