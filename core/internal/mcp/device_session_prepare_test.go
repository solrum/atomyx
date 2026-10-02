package mcp

import (
	"context"
	"testing"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// hangingPreparer blocks until its context ends, as a build stuck on a
// lock another process never releases does.
type hangingPreparer struct{}

func (p hangingPreparer) Prepare(ctx context.Context, _ device.Descriptor) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestSelectReportsRunnerBuildTimeoutNotToolTimeout(t *testing.T) {
	f := newLifecycleFixture(t)
	hung := f.newDevicesPreparing("atomyx-mcp", f.factory, hangingPreparer{})
	t.Cleanup(hung.Close)
	go func() {
		f.clk.AwaitSleepers(1)
		f.clk.Advance(device.DefaultRunnerBuildTimeoutMs)
	}()

	_, err := hung.Select(context.Background(), SelectDeviceInput{Platform: device.PlatformAndroid, ID: "A"})

	if got := codeOfErr(err); got != diag.CodeRunnerBuildTimeout {
		t.Fatalf("Select() code = %q, want %s (error: %v)", got, diag.CodeRunnerBuildTimeout, err)
	}
	if f.factory.builds() != 0 || f.leaseHeld() {
		t.Fatal("a build that timed out still built a driver or took the device lease")
	}
}

type recordedPhase struct {
	progress float64
	total    float64
	message  string
}

func progressClient(t *testing.T, got chan<- recordedPhase) (*mcp.ClientSession, context.Context) {
	t.Helper()
	ctx := context.Background()
	server := NewServer(Info{Name: "atomyx-test", Version: "0.0.0-test"}, ServerDeps{
		Probe: device.NewStaticProbe(), Factory: device.NewMockDriverFactory(), Storage: storage.NewInMemoryStorage(),
	})
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "atomyx-test-client", Version: "0.0.0-test"}, &mcp.ClientOptions{
		ProgressNotificationHandler: func(_ context.Context, req *mcp.ProgressNotificationClientRequest) {
			got <- recordedPhase{progress: req.Params.Progress, total: req.Params.Total, message: req.Params.Message}
		},
	})
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs, ctx
}

func selectParams(token any) *mcp.CallToolParams {
	params := &mcp.CallToolParams{Name: "select_device", Arguments: map[string]any{"platform": "android", "id": "emulator-5554"}}
	if token != nil {
		params.SetProgressToken(token)
	}
	return params
}

func TestSelectDeviceSendsProgressInOrderWhenTheCallHasAToken(t *testing.T) {
	got := make(chan recordedPhase, 8)
	cs, ctx := progressClient(t, got)

	if _, err := cs.CallTool(ctx, selectParams("tok-1")); err != nil {
		t.Fatal(err)
	}

	want := []recordedPhase{
		{1, acquirePhaseCount, phaseBuilding.message},
		{2, acquirePhaseCount, phaseLaunching.message},
		{3, acquirePhaseCount, phaseConnected.message},
	}
	for i, w := range want {
		select {
		case phase := <-got:
			if phase != w {
				t.Fatalf("notification %d = %+v, want %+v", i, phase, w)
			}
		default:
			t.Fatalf("notification %d missing, want %+v", i, w)
		}
	}
	if extra := len(got); extra != 0 {
		t.Fatalf("%d extra progress notifications, want exactly three", extra)
	}
}

func TestSelectDeviceSendsNoProgressWithoutAToken(t *testing.T) {
	got := make(chan recordedPhase, 8)
	cs, ctx := progressClient(t, got)

	if _, err := cs.CallTool(ctx, selectParams(nil)); err != nil {
		t.Fatal(err)
	}

	if n := len(got); n != 0 {
		t.Fatalf("%d progress notifications without a progress token, want none", n)
	}
}
