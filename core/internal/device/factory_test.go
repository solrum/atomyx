package device

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"runtime"
	"testing"

	"dev.atomyx/internal/iosdriver"
	"dev.atomyx/internal/iossimdriver"
	"dev.atomyx/internal/process"
)

func TestMockDriverFactory_Build_ReturnsConnectedMockDriverAndOrchestra(t *testing.T) {
	f := NewMockDriverFactory()
	built, err := f.Build(context.Background(), Descriptor{ID: "d1", Platform: PlatformAndroid})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if built.Driver == nil || built.Orchestra == nil {
		t.Fatalf("Build() = %+v, want a non-nil Driver and Orchestra", built)
	}
	if !built.Driver.IsConnected() {
		t.Fatal("Build() returned a Driver that is not connected")
	}
}

// fakeSimHidStarter is a device-free simHidStarter test double for
// DriverFactory tests: it records every UDID it was asked to start
// and returns a staged port or error, with no launcher, process, or
// real helper involved.
type fakeSimHidStarter struct {
	port  int
	err   error
	calls []string
}

func (f *fakeSimHidStarter) StartSimHid(_ context.Context, udid string) (int, error) {
	f.calls = append(f.calls, udid)
	return f.port, f.err
}

// setEnv sets key to value for the duration of the test, restoring
// whatever the environment held before.
func setEnv(t *testing.T, key, value string) {
	t.Helper()
	saved, had := os.LookupEnv(key)
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, saved)
		} else {
			_ = os.Unsetenv(key)
		}
	})
	_ = os.Setenv(key, value)
}

// stubArm64XcodeFloor stages runner so
// iossimdriver.IsSimDirectSupported reports true, and resets its
// memoized cache before and after the test so no other test observes
// a stale result.
func stubArm64XcodeFloor(t *testing.T, runner *process.Fake) {
	t.Helper()
	iossimdriver.ResetSimDirectCacheForTests()
	t.Cleanup(iossimdriver.ResetSimDirectCacheForTests)
	runner.Stub(process.ExactMatch("xcode-select", "-p"), process.Result{Stdout: "/Applications/Xcode.app/Contents/Developer\n"}, nil)
	runner.Stub(
		process.ExactMatch("plutil", "-extract", "DTXcode", "raw", "/Applications/Xcode.app/Contents/Info.plist"),
		process.Result{Stdout: "1620\n"},
		nil,
	)
}

func TestDriverFactory_CreateDriver_SimHidOptInUnset_BuildsIosDriverWithoutStartingHelper(t *testing.T) {
	setEnv(t, simHidOptInEnv, "")
	starter := &fakeSimHidStarter{port: 4242}
	runner := process.NewFake()
	f := NewDriverFactory(DriverFactoryDeps{Runner: runner, Background: process.NewFakeBackground(), SimHid: starter})

	drv, err := f.createDriver(context.Background(), Descriptor{ID: "sim-1", Platform: PlatformIOS, Kind: KindSimulator})
	if err != nil {
		t.Fatalf("createDriver() error = %v", err)
	}
	if _, ok := drv.(*iosdriver.IosDriver); !ok {
		t.Fatalf("createDriver() = %T, want *iosdriver.IosDriver when ATOMYX_SIM_HID is unset", drv)
	}
	if len(starter.calls) != 0 {
		t.Fatalf("StartSimHid calls = %v, want none when ATOMYX_SIM_HID is unset", starter.calls)
	}
	if len(runner.Invocations()) != 0 {
		t.Fatalf("Runner invocations = %+v, want none: the env gate short-circuits before probing Xcode", runner.Invocations())
	}
}

func TestDriverFactory_CreateDriver_SimHidOptInSetAndSupported_StartsHelperAndBuildsIosSimDriver(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("requires an arm64 host to reach the Xcode version probe IsSimDirectSupported performs")
	}
	setEnv(t, simHidOptInEnv, "1")
	runner := process.NewFake()
	stubArm64XcodeFloor(t, runner)
	starter := &fakeSimHidStarter{port: 19876}
	f := NewDriverFactory(DriverFactoryDeps{Runner: runner, Background: process.NewFakeBackground(), SimHid: starter})

	drv, err := f.createDriver(context.Background(), Descriptor{ID: "sim-1", Platform: PlatformIOS, Kind: KindSimulator, Port: unusedPort(t)})
	if err != nil {
		t.Fatalf("createDriver() error = %v", err)
	}
	if _, ok := drv.(*iossimdriver.IosSimDriver); !ok {
		t.Fatalf("createDriver() = %T, want *iossimdriver.IosSimDriver", drv)
	}
	if len(starter.calls) != 1 || starter.calls[0] != "sim-1" {
		t.Fatalf("StartSimHid calls = %v, want exactly one call for sim-1", starter.calls)
	}
}

func TestDriverFactory_CreateDriver_SimHidStartFails_FallsBackToIosDriver(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("requires an arm64 host to reach the Xcode version probe IsSimDirectSupported performs")
	}
	setEnv(t, simHidOptInEnv, "1")
	runner := process.NewFake()
	stubArm64XcodeFloor(t, runner)
	starter := &fakeSimHidStarter{err: context.DeadlineExceeded}
	f := NewDriverFactory(DriverFactoryDeps{Runner: runner, Background: process.NewFakeBackground(), SimHid: starter})

	drv, err := f.createDriver(context.Background(), Descriptor{ID: "sim-1", Platform: PlatformIOS, Kind: KindSimulator})
	if err != nil {
		t.Fatalf("createDriver() error = %v, want a fallback IosDriver instead of an error", err)
	}
	if _, ok := drv.(*iosdriver.IosDriver); !ok {
		t.Fatalf("createDriver() = %T, want *iosdriver.IosDriver as the fallback when StartSimHid fails", drv)
	}
	if len(starter.calls) != 1 {
		t.Fatalf("StartSimHid calls = %v, want exactly one attempt before falling back", starter.calls)
	}
}

func TestDriverFactory_CreateDriver_NoSimHidManagerInjected_BuildsIosDriverEvenWithOptIn(t *testing.T) {
	setEnv(t, simHidOptInEnv, "1")
	runner := process.NewFake()
	f := NewDriverFactory(DriverFactoryDeps{Runner: runner, Background: process.NewFakeBackground()})

	drv, err := f.createDriver(context.Background(), Descriptor{ID: "sim-1", Platform: PlatformIOS, Kind: KindSimulator})
	if err != nil {
		t.Fatalf("createDriver() error = %v", err)
	}
	if _, ok := drv.(*iosdriver.IosDriver); !ok {
		t.Fatalf("createDriver() = %T, want *iosdriver.IosDriver when no SimHidManager is injected", drv)
	}
}

func TestDriverFactory_CreateDriver_ExplicitHidPort_BuildsIosSimDriverWithoutStarter(t *testing.T) {
	starter := &fakeSimHidStarter{}
	f := NewDriverFactory(DriverFactoryDeps{Runner: process.NewFake(), Background: process.NewFakeBackground(), SimHid: starter})

	drv, err := f.createDriver(context.Background(), Descriptor{ID: "sim-1", Platform: PlatformIOS, Kind: KindSimulator, Port: unusedPort(t), HidPort: 7777})
	if err != nil {
		t.Fatalf("createDriver() error = %v", err)
	}
	if _, ok := drv.(*iossimdriver.IosSimDriver); !ok {
		t.Fatalf("createDriver() = %T, want *iossimdriver.IosSimDriver for an explicit HidPort", drv)
	}
	if len(starter.calls) != 0 {
		t.Fatalf("StartSimHid calls = %v, want none when the descriptor already carries a HidPort", starter.calls)
	}
}

// listeningBackground is a BackgroundRunner whose Start binds a
// listener that drops every connection, modeling an iproxy tunnel
// that comes up but forwards to an agent that never answers.
type listeningBackground struct {
	t    *testing.T
	port int
	proc *process.FakeBackgroundProcess
}

func (b *listeningBackground) Start(ctx context.Context, _ string, _ ...string) (process.BackgroundProcess, error) {
	l, err := new(net.ListenConfig).Listen(ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", b.port))
	if err != nil {
		return nil, err
	}
	b.t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	return b.proc, nil
}

func TestDriverFactory_Build_ConnectFailsAfterTunnelStarted_StopsTheTunnel(t *testing.T) {
	probe, err := new(net.ListenConfig).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	_ = probe.Close()

	proc := process.NewFakeBackgroundProcess()
	f := NewDriverFactory(DriverFactoryDeps{
		Runner:     process.NewFake(),
		Background: &listeningBackground{t: t, port: port, proc: proc},
	})

	_, err = f.Build(context.Background(), Descriptor{ID: "DEV-1", Platform: PlatformIOS, Kind: KindDevice, Port: port})
	if err == nil {
		t.Fatal("Build() succeeded against an agent that never answers the handshake")
	}
	if !proc.Killed() {
		t.Fatal("the tunnel process was left running after Build failed")
	}
}

func TestNewDriverFactoryPanicsWithoutABackgroundRunner(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NewDriverFactory accepted a nil Background")
		}
	}()
	NewDriverFactory(DriverFactoryDeps{Runner: process.NewFake()})
}

func TestNewDriverProvisionerPanicsWithoutABackgroundRunner(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NewDriverProvisioner accepted a nil Background")
		}
	}()
	NewDriverProvisioner(DriverProvisionerDeps{Runner: process.NewFake()})
}

// serveRunnerReply starts a listener that answers every ping with
// the given identity fields, the inner part of the reply's data
// object, and returns its port.
func serveRunnerReply(t *testing.T, identityFields string) int {
	t.Helper()
	l, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				buf := make([]byte, 256)
				if _, err := c.Read(buf); err != nil {
					return
				}
				_, _ = fmt.Fprintf(c, `{"id":0,"ok":true,"data":{"pong":true,%s}}`+"\n", withHostProtocol(identityFields))
			}(c)
		}
	}()
	return l.Addr().(*net.TCPAddr).Port
}

func TestDriverFactory_CreateDriver_HidPath_RefusesRunnerOfAnotherSimulator(t *testing.T) {
	port := serveRunnerReply(t, `"simulatorUdid":"sim-2"`)
	f := NewDriverFactory(DriverFactoryDeps{Runner: process.NewFake(), Background: process.NewFakeBackground()})

	_, err := f.createDriver(context.Background(), Descriptor{
		ID: "sim-1", Platform: PlatformIOS, Kind: KindSimulator, Port: port, HidPort: 7777,
	})
	var idErr *iosdriver.RunnerIdentityError
	if !errors.As(err, &idErr) {
		t.Fatalf("createDriver() error = %v, want *iosdriver.RunnerIdentityError for a runner of another simulator", err)
	}
}

func TestDriverFactory_CreateDriver_HidPath_AcceptsRunnerOfTheRequestedSimulator(t *testing.T) {
	port := serveRunnerReply(t, `"simulatorUdid":"SIM-1"`)
	f := NewDriverFactory(DriverFactoryDeps{Runner: process.NewFake(), Background: process.NewFakeBackground()})

	drv, err := f.createDriver(context.Background(), Descriptor{
		ID: "sim-1", Platform: PlatformIOS, Kind: KindSimulator, Port: port, HidPort: 7777,
	})
	if err != nil {
		t.Fatalf("createDriver() error = %v", err)
	}
	if _, ok := drv.(*iossimdriver.IosSimDriver); !ok {
		t.Fatalf("createDriver() = %T, want *iossimdriver.IosSimDriver", drv)
	}
}

func TestDriverFactory_CreateDriver_HidPath_RefusesRunnerWithoutIdentity(t *testing.T) {
	port := serveRunnerReply(t, `"extra":1`)
	f := NewDriverFactory(DriverFactoryDeps{Runner: process.NewFake(), Background: process.NewFakeBackground()})

	_, err := f.createDriver(context.Background(), Descriptor{
		ID: "sim-1", Platform: PlatformIOS, Kind: KindSimulator, Port: port, HidPort: 7777,
	})
	var idErr *iosdriver.RunnerIdentityError
	if !errors.As(err, &idErr) {
		t.Fatalf("createDriver() error = %v, want a RunnerIdentityError for a runner that proves no identity", err)
	}
}

func TestDriverFactory_CreateDriver_ServedRunnerWithoutIdentityConnectsWhenUnverified(t *testing.T) {
	port := serveConnectedRunner(t, `"extra":1`)
	f := NewDriverFactory(DriverFactoryDeps{Runner: process.NewFake(), Background: process.NewFakeBackground()})

	drv, err := f.createDriver(context.Background(), Descriptor{
		ID: "sim-1", Platform: PlatformIOS, Kind: KindSimulator, Port: port,
	})
	if err != nil {
		t.Fatalf("createDriver() error = %v", err)
	}
	if err := drv.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v, want a hand-served runner that reports no udid to connect", err)
	}
}

func TestDriverFactory_CreateDriver_VerifiedRunnerIsRecheckedAtConnect(t *testing.T) {
	port := serveConnectedRunner(t, `"simulatorUdid":"sim-2"`)
	f := NewDriverFactory(DriverFactoryDeps{Runner: process.NewFake(), Background: process.NewFakeBackground()})

	drv, err := f.createDriver(context.Background(), Descriptor{
		ID: "sim-1", Platform: PlatformIOS, Kind: KindSimulator, Port: port, RunnerVerified: true,
	})
	if err != nil {
		t.Fatalf("createDriver() error = %v", err)
	}
	var idErr *iosdriver.RunnerIdentityError
	if err := drv.Connect(context.Background()); !errors.As(err, &idErr) {
		t.Fatalf("Connect() error = %v, want *iosdriver.RunnerIdentityError for a swapped runner", err)
	}
}

// serveConnectedRunner serves a persistent connection that answers every
// request with a pong carrying identityFields and the request's own id.
func serveConnectedRunner(t *testing.T, identityFields string) int {
	t.Helper()
	l, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				r := bufio.NewReader(c)
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					var req struct {
						ID int64 `json:"id"`
					}
					_ = json.Unmarshal([]byte(line), &req)
					_, _ = fmt.Fprintf(c, `{"id":%d,"ok":true,"data":{"pong":true,%s}}`+"\n", req.ID, withHostProtocol(identityFields))
				}
			}(c)
		}
	}()
	return l.Addr().(*net.TCPAddr).Port
}
