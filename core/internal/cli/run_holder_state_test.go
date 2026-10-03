package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"sync"
	"testing"

	"dev.atomyx/internal/device"
	mockdriver "dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/lease"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/session/sessiontest"
)

// holderObservation is what a probe saw while a command drove the device.
type holderObservation struct {
	known bool
	idle  bool
	runID string
}

// holderProbe records the holder's state and lease run id every time the
// device is called, so a test sees them from inside the command.
type holderProbe struct {
	mu       sync.Mutex
	manager  *lease.Manager
	observed []holderObservation
}

func (p *holderProbe) observe(ctx context.Context) {
	var obs holderObservation
	status, err := p.manager.Probe(ctx, lease.KeyFor("android", "emulator-5554"))
	if err == nil && status.HolderKnown {
		obs.known = true
		obs.idle = status.Holder.IdleSince != nil
		obs.runID = status.Holder.RunID
	}
	p.mu.Lock()
	p.observed = append(p.observed, obs)
	p.mu.Unlock()
}

type probingDriver struct {
	*mockdriver.Driver
	probe *holderProbe
}

func (d *probingDriver) Ping(ctx context.Context) error {
	d.probe.observe(ctx)
	return d.Driver.Ping(ctx)
}

func (d *probingDriver) Screenshot(ctx context.Context) ([]byte, error) {
	d.probe.observe(ctx)
	return d.Driver.Screenshot(ctx)
}

type probingFactory struct{ probe *holderProbe }

func (f probingFactory) WithDriverWrap(wrap device.DriverWrap) device.Factory {
	return sessiontest.Wrapping(f).WithDriverWrap(wrap)
}

func (f probingFactory) Build(ctx context.Context, _ device.Descriptor) (device.Built, error) {
	drv := &probingDriver{Driver: mockdriver.New(), probe: f.probe}
	if err := drv.Connect(ctx); err != nil {
		return device.Built{}, err
	}
	return device.Built{
		Driver:    drv,
		Orchestra: orchestra.New(orchestra.OrchestraDeps{Driver: drv, Clock: orchestra.SystemClock{}}),
	}, nil
}

func probedDeps(t *testing.T, stdout, stderr *bytes.Buffer) (Deps, *holderProbe) {
	t.Helper()
	probe := &holderProbe{manager: newLeaseManager(t)}
	deps := leaseDeps(t, probe.manager, stdout, stderr)
	deps.Factory = probingFactory{probe: probe}
	return deps, probe
}

func requireAllRunning(t *testing.T, probe *holderProbe) holderObservation {
	t.Helper()
	probe.mu.Lock()
	defer probe.mu.Unlock()
	if len(probe.observed) == 0 {
		t.Fatal("the command never called the device")
	}
	for i, obs := range probe.observed {
		if !obs.known {
			t.Errorf("observation %d: the lease holder record was unreadable", i)
		} else if obs.idle {
			t.Errorf("observation %d: lease holder is idle, want running for the whole command", i)
		}
	}
	return probe.observed[len(probe.observed)-1]
}

func TestRunHoldsTheDeviceAsRunningWithItsRunIdInTheLease(t *testing.T) {
	var stdout, stderr bytes.Buffer
	deps, probe := probedDeps(t, &stdout, &stderr)
	scriptPath := writeScript(t, "flow.yml", "- screenshot: home\n")
	flags := map[string]FlagValue{"--file": scriptPath, "--report-dir": t.TempDir(), "--platform": "android", "--device": "emulator-5554"}

	if code := RunScript(context.Background(), deps, flags); code != 0 {
		t.Fatalf("exit code = %d, stderr: %s", code, stderr.String())
	}

	if last := requireAllRunning(t, probe); last.runID == "" {
		t.Errorf("lease runId is empty, want the open run's id once the run record exists")
	}
}

func TestRunSuiteHoldsTheDeviceAsRunningWithItsRunIdInTheLease(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, dir, "pass.yml", "- screenshot: home\n")
	var stdout, stderr bytes.Buffer
	deps, probe := probedDeps(t, &stdout, &stderr)
	flags := map[string]FlagValue{"--glob": []string{"*.yml"}, "--report-dir": filepath.Join(dir, "reports"), "--platform": "android", "--device": "emulator-5554"}

	if code := RunSuite(context.Background(), deps, flags, nil); code != 0 {
		t.Fatalf("exit code = %d, stderr: %s", code, stderr.String())
	}

	if last := requireAllRunning(t, probe); last.runID == "" {
		t.Errorf("lease runId is empty, want the open run's id once the run record exists")
	}
}

func TestDoctorHoldsTheDeviceAsRunning(t *testing.T) {
	var stdout, stderr bytes.Buffer
	deps, probe := probedDeps(t, &stdout, &stderr)

	if code := Doctor(context.Background(), deps, heldDevice); code != 0 {
		t.Fatalf("exit code = %d, stderr: %s", code, stderr.String())
	}

	requireAllRunning(t, probe)
}
