package mcp

import (
	"context"
	"sync"
	"testing"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/diag"
	mockdriver "dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/session/sessiontest"
	"dev.atomyx/internal/storage"
	"dev.atomyx/internal/version"
)

const (
	servedTestUDID = "00008101-AAAA"
	servedTestPort = 22087
)

type servedRunnerStub struct {
	runner device.ServedRunner
}

func (s servedRunnerStub) IdentifyRunner(context.Context, int) (device.ServedRunner, bool) {
	return s.runner, true
}

type descriptorRecordingFactory struct {
	mu    sync.Mutex
	descs []device.Descriptor
}

func (f *descriptorRecordingFactory) WithDriverWrap(wrap device.DriverWrap) device.Factory {
	return sessiontest.Wrapping(f).WithDriverWrap(wrap)
}

func (f *descriptorRecordingFactory) Build(ctx context.Context, desc device.Descriptor) (device.Built, error) {
	f.mu.Lock()
	f.descs = append(f.descs, desc)
	f.mu.Unlock()
	return singleMockDriverFactory{driver: mockdriver.New()}.Build(ctx, desc)
}

func (f *descriptorRecordingFactory) built(t *testing.T) device.Descriptor {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.descs) != 1 {
		t.Fatalf("factory builds = %d, want 1", len(f.descs))
	}
	return f.descs[0]
}

func newServedRunnerServer(t *testing.T, runner device.ServedRunner, factory device.WrappingFactory) *Server {
	t.Helper()
	probe := device.NewStaticProbe([]device.Descriptor{
		{ID: servedTestUDID, Platform: device.PlatformIOS, Name: "iPhone", Kind: device.KindDevice, State: device.StateOnline},
	})
	return mustNewServer(t, ServerDeps{
		Probe:   probe,
		Runners: servedRunnerStub{runner: runner},
		Factory: factory,
		Storage: storage.NewInMemoryStorage(),
	})
}

func matchingServedRunner() device.ServedRunner {
	return device.ServedRunner{TargetUDID: servedTestUDID, ProtocolVersion: version.AgentProtocolVersion}
}

func TestSelectDevice_OmittedPortReusesAMatchingServedRunner(t *testing.T) {
	factory := &descriptorRecordingFactory{}
	server := newServedRunnerServer(t, matchingServedRunner(), factory)

	if _, err := server.session.Select(context.Background(), SelectDeviceInput{Platform: device.PlatformIOS, ID: servedTestUDID}); err != nil {
		t.Fatalf("Select: %v", err)
	}

	if desc := factory.built(t); desc.Port != servedTestPort || !desc.RunnerVerified {
		t.Fatalf("built descriptor = %+v, want port %d with the runner verified", desc, servedTestPort)
	}
}

func TestSelectDevice_ExplicitDifferentPortClearsRunnerVerification(t *testing.T) {
	factory := &descriptorRecordingFactory{}
	server := newServedRunnerServer(t, matchingServedRunner(), factory)

	const otherPort = 23000
	if _, err := server.session.Select(context.Background(), SelectDeviceInput{Platform: device.PlatformIOS, ID: servedTestUDID, Port: otherPort}); err != nil {
		t.Fatalf("Select: %v", err)
	}

	if desc := factory.built(t); desc.Port != otherPort || desc.RunnerVerified {
		t.Fatalf("built descriptor = %+v, want port %d with no verification", desc, otherPort)
	}
}

func TestSelectDevice_ExplicitServedPortKeepsRunnerVerification(t *testing.T) {
	factory := &descriptorRecordingFactory{}
	server := newServedRunnerServer(t, matchingServedRunner(), factory)

	if _, err := server.session.Select(context.Background(), SelectDeviceInput{Platform: device.PlatformIOS, ID: servedTestUDID, Port: servedTestPort}); err != nil {
		t.Fatalf("Select: %v", err)
	}

	if desc := factory.built(t); desc.Port != servedTestPort || !desc.RunnerVerified {
		t.Fatalf("built descriptor = %+v, want port %d with the runner verified", desc, servedTestPort)
	}
}

func TestSelectDevice_ServedRunnerProtocolMismatchSurfacesAgentVersionMismatch(t *testing.T) {
	factory := &descriptorRecordingFactory{}
	runner := matchingServedRunner()
	runner.ProtocolVersion = version.AgentProtocolVersion + 1
	server := newServedRunnerServer(t, runner, factory)

	_, err := server.session.Select(context.Background(), SelectDeviceInput{Platform: device.PlatformIOS, ID: servedTestUDID})

	if code, _, _ := diag.Describe(err); code != diag.CodeAgentVersionMismatch {
		t.Fatalf("Select error = %v, code %q, want %s", err, code, diag.CodeAgentVersionMismatch)
	}
	factory.mu.Lock()
	defer factory.mu.Unlock()
	if len(factory.descs) != 0 {
		t.Fatalf("factory builds = %d, want none after a refused selection", len(factory.descs))
	}
}
