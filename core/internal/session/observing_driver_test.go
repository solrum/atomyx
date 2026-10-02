package session_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"testing"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/driver"
	mockdriver "dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/session"
	"dev.atomyx/internal/wire"
)

type methodClass int

const (
	classPassive methodClass = iota
	classObserving
	classMutating
)

// driverMethodClasses classifies every driver.Driver method. A method
// added to the port has no entry here, which fails the classification
// test until its author decides whether it mutates the device.
var driverMethodClasses = map[string]methodClass{
	"Platform":          classPassive,
	"Capabilities":      classPassive,
	"Connect":           classPassive,
	"Disconnect":        classPassive,
	"IsConnected":       classPassive,
	"Ping":              classPassive,
	"WaitForIdle":       classPassive,
	"CurrentForeground": classPassive,
	"ListApps":          classPassive,
	"IsAppInstalled":    classPassive,
	"DeviceInfo":        classPassive,
	"Hierarchy":         classObserving,
	"Screenshot":        classObserving,
	"ScreenSize":        classObserving,
	"Tap":               classMutating,
	"LongPress":         classMutating,
	"Swipe":             classMutating,
	"DispatchGesture":   classMutating,
	"InputText":         classMutating,
	"EraseText":         classMutating,
	"PressKey":          classMutating,
	"HideKeyboard":      classMutating,
	"LaunchApp":         classMutating,
	"StopApp":           classMutating,
	"KillApp":           classMutating,
	"InstallApp":        classMutating,
	"UninstallApp":      classMutating,
	"ClearAppData":      classMutating,
}

var streamingMethodClasses = map[string]methodClass{
	"StreamingTouchDown": classMutating,
	"StreamingTouchMove": classMutating,
	"StreamingTouchUp":   classMutating,
}

var carrierMethodClasses = map[string]methodClass{
	"LaunchedApp":        classPassive,
	"RestoreLaunchedApp": classMutating,
}

func methodNames(t reflect.Type) []string {
	names := make([]string, 0, t.NumMethod())
	for i := 0; i < t.NumMethod(); i++ {
		names = append(names, t.Method(i).Name)
	}
	sort.Strings(names)
	return names
}

func assertClassified(t *testing.T, iface reflect.Type, table map[string]methodClass) {
	t.Helper()
	for _, name := range methodNames(iface) {
		if _, ok := table[name]; !ok {
			t.Errorf("%s.%s is not classified as mutating, observing or passive", iface.Name(), name)
		}
	}
	for name := range table {
		if _, ok := iface.MethodByName(name); !ok {
			t.Errorf("classification lists %s, which %s does not declare", name, iface.Name())
		}
	}
}

func TestObservingDriver_EveryPortMethodIsClassified(t *testing.T) {
	assertClassified(t, reflect.TypeFor[driver.Driver](), driverMethodClasses)
	assertClassified(t, reflect.TypeFor[driver.StreamingTouchCapable](), streamingMethodClasses)
}

// invoke calls the named method of target with a live context and zero
// values for every other argument.
func invoke(t *testing.T, target any, name string) {
	t.Helper()
	method := reflect.ValueOf(target).MethodByName(name)
	if !method.IsValid() {
		t.Fatalf("%T has no method %s", target, name)
	}
	ctxType := reflect.TypeFor[context.Context]()
	args := make([]reflect.Value, method.Type().NumIn())
	for i := range args {
		in := method.Type().In(i)
		if in == ctxType {
			args[i] = reflect.ValueOf(context.Background())
		} else {
			args[i] = reflect.Zero(in)
		}
	}
	method.Call(args)
}

func assertEpochBehavior(t *testing.T, target session.ObservedDriver, table map[string]methodClass) {
	t.Helper()
	for name, class := range table {
		before := target.Epoch()
		invoke(t, target, name)
		advanced := target.Epoch() != before
		if want := class == classMutating; advanced != want {
			t.Errorf("%s: epoch advanced = %v, want %v", name, advanced, want)
		}
	}
}

func TestObservingDriver_EpochAdvancesExactlyOnMutatingCalls(t *testing.T) {
	assertEpochBehavior(t, session.NewObservingDriver(mockdriver.New(), nil), driverMethodClasses)
}

func TestObservingDriver_StreamingAndCarrierMethodsFollowTheirClass(t *testing.T) {
	streaming := session.NewObservingDriver(mockdriver.NewStreamingTouchCapable(), nil)
	assertEpochBehavior(t, streaming, streamingMethodClasses)

	both := session.NewObservingDriver(&carrierStreamingDriver{StreamingDriver: mockdriver.NewStreamingTouchCapable()}, nil)
	assertEpochBehavior(t, both, streamingMethodClasses)
	assertEpochBehavior(t, both, carrierMethodClasses)
}

type carrierDriver struct {
	*mockdriver.Driver
	bundleID, runnerID string
	restored           int
}

func (d *carrierDriver) LaunchedApp() (string, string) { return d.bundleID, d.runnerID }

func (d *carrierDriver) RestoreLaunchedApp(context.Context, string, string) error {
	d.restored++
	return nil
}

type carrierStreamingDriver struct {
	*mockdriver.StreamingDriver
	restored int
}

func (d *carrierStreamingDriver) LaunchedApp() (string, string) { return "app", "runner" }

func (d *carrierStreamingDriver) RestoreLaunchedApp(context.Context, string, string) error {
	d.restored++
	return nil
}

func TestObservingDriver_OptionalInterfacesAreForwardedExactlyWhenTheDriverHasThem(t *testing.T) {
	type carrier interface {
		LaunchedApp() (string, string)
		RestoreLaunchedApp(context.Context, string, string) error
	}
	cases := []struct {
		name          string
		inner         driver.Driver
		wantStreaming bool
		wantCarrier   bool
	}{
		{"plain", mockdriver.New(), false, false},
		{"streaming", mockdriver.NewStreamingTouchCapable(), true, false},
		{"carrier", &carrierDriver{Driver: mockdriver.New(), bundleID: "b", runnerID: "r"}, false, true},
		{"both", &carrierStreamingDriver{StreamingDriver: mockdriver.NewStreamingTouchCapable()}, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wrapped := session.NewObservingDriver(tc.inner, nil)
			_, gotStreaming := wrapped.(driver.StreamingTouchCapable)
			_, gotCarrier := wrapped.(carrier)
			if gotStreaming != tc.wantStreaming || gotCarrier != tc.wantCarrier {
				t.Fatalf("wrapped streaming=%v carrier=%v, want streaming=%v carrier=%v", gotStreaming, gotCarrier, tc.wantStreaming, tc.wantCarrier)
			}
			_, seen := driver.AsStreamingTouchCapable(wrapped)
			if seen != tc.wantStreaming {
				t.Fatalf("AsStreamingTouchCapable = %v, want %v", seen, tc.wantStreaming)
			}
		})
	}
}

func TestObservingDriver_ForwardedCarrierReachesTheInnerDriver(t *testing.T) {
	inner := &carrierDriver{Driver: mockdriver.New(), bundleID: "com.example", runnerID: "run-1"}
	wrapped := session.NewObservingDriver(inner, nil)

	got, ok := driver.As[interface{ LaunchedApp() (string, string) }](wrapped)
	if !ok {
		t.Fatal("wrapped driver does not expose LaunchedApp")
	}
	if bundle, runner := got.LaunchedApp(); bundle != "com.example" || runner != "run-1" {
		t.Fatalf("LaunchedApp = %q, %q", bundle, runner)
	}
	restorer := wrapped.(interface {
		RestoreLaunchedApp(context.Context, string, string) error
	})
	if err := restorer.RestoreLaunchedApp(context.Background(), "com.example", "run-1"); err != nil || inner.restored != 1 {
		t.Fatalf("restore err = %v, inner restores = %d", err, inner.restored)
	}
}

func TestObservingDriver_UnwrapReachesTheInnerDriver(t *testing.T) {
	inner := mockdriver.New()
	wrapped := session.NewObservingDriver(inner, nil)
	if wrapped.Unwrap() != driver.Driver(inner) {
		t.Fatal("Unwrap did not return the wrapped driver")
	}
}

func TestDriverAs_FollowsUnwrapAndReportsAbsence(t *testing.T) {
	type unrelated interface{ NotImplemented() }
	inner := &carrierDriver{Driver: mockdriver.New()}
	wrapped := session.NewObservingDriver(mockdriver.New(), nil)
	hidden := &hidingDriver{Driver: session.NewObservingDriver(inner, nil)}

	if _, ok := driver.As[interface{ LaunchedApp() (string, string) }](hidden); !ok {
		t.Fatal("As did not look through a decorator that hides the capability")
	}
	if _, ok := driver.As[unrelated](wrapped); ok {
		t.Fatal("As reported a capability nothing implements")
	}
	if _, ok := driver.As[unrelated](nil); ok {
		t.Fatal("As reported a capability on a nil driver")
	}
}

// hidingDriver decorates a driver without forwarding its optional
// interfaces, so only Unwrap reaches them.
type hidingDriver struct{ driver.Driver }

func (h *hidingDriver) Unwrap() driver.Driver { return h.Driver }

type captureRecord struct {
	kind  string
	epoch uint64
}

type recordingObserver struct {
	mu      sync.Mutex
	records []captureRecord
	trees   []wire.TreeNodeWire
	images  [][]byte
	sizes   []wire.Size
}

func (r *recordingObserver) ObserveTree(tree wire.TreeNodeWire, epoch uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, captureRecord{"tree", epoch})
	r.trees = append(r.trees, tree)
}

func (r *recordingObserver) ObserveScreenshot(image []byte, epoch uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, captureRecord{"screenshot", epoch})
	r.images = append(r.images, image)
}

func (r *recordingObserver) ObserveScreenSize(size wire.Size, epoch uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, captureRecord{"size", epoch})
	r.sizes = append(r.sizes, size)
}

func TestObservingDriver_ReportsCapturesWithTheEpochActiveWhenFetched(t *testing.T) {
	ctx := context.Background()
	inner := mockdriver.New()
	inner.ScreenshotData = []byte("png")
	inner.StageHierarchy(wire.TreeNodeWire{Attributes: map[string]string{"id": "root"}})
	observer := &recordingObserver{}
	wrapped := session.NewObservingDriver(inner, observer)

	first := wrapped.Epoch()
	_, _ = wrapped.Hierarchy(ctx)
	_, _ = wrapped.Screenshot(ctx)
	_, _ = wrapped.ScreenSize(ctx)
	_ = wrapped.Tap(ctx, wire.Point{X: 1, Y: 2})
	_, _ = wrapped.Screenshot(ctx)

	want := []captureRecord{{"tree", first}, {"screenshot", first}, {"size", first}, {"screenshot", wrapped.Epoch()}}
	if !reflect.DeepEqual(observer.records, want) {
		t.Fatalf("records = %+v, want %+v", observer.records, want)
	}
	if want[3].epoch == first {
		t.Fatal("a capture after a tap shares the epoch of one before it")
	}
	if observer.trees[0].Attributes["id"] != "root" || string(observer.images[0]) != "png" || observer.sizes[0] != inner.Screen {
		t.Fatalf("observer saw %+v %q %+v, want the values the driver returned", observer.trees[0], observer.images[0], observer.sizes[0])
	}
}

func TestObservingDriver_DoesNotReportAFailedCapture(t *testing.T) {
	inner := mockdriver.New()
	inner.ScreenshotErr = errors.New("no screen")
	observer := &recordingObserver{}
	wrapped := session.NewObservingDriver(inner, observer)

	if _, err := wrapped.Screenshot(context.Background()); err == nil {
		t.Fatal("Screenshot swallowed the error")
	}
	if len(observer.records) != 0 {
		t.Fatalf("observer saw %+v for a failed capture", observer.records)
	}
}

// failingDriver returns err from every mutating call.
type failingDriver struct {
	*mockdriver.Driver
	err error
}

func (d *failingDriver) Tap(context.Context, wire.Point) error { return d.err }

func (d *failingDriver) Hierarchy(context.Context) (wire.TreeNodeWire, error) {
	return wire.TreeNodeWire{}, d.err
}

func TestObservingDriver_ErrorsPassThroughUnchanged(t *testing.T) {
	exit := &device.ProcessExitError{Component: device.LostRunner, Cause: errors.New("signal: killed")}
	cases := []struct {
		name string
		err  error
		is   error
	}{
		{"process exit", fmt.Errorf("tap failed: %w", exit), nil},
		{"canceled", context.Canceled, context.Canceled},
		{"deadline", fmt.Errorf("slow: %w", context.DeadlineExceeded), context.DeadlineExceeded},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wrapped := session.NewObservingDriver(&failingDriver{Driver: mockdriver.New(), err: tc.err}, nil)
			for label, call := range map[string]func() error{
				"Tap":       func() error { return wrapped.Tap(context.Background(), wire.Point{}) },
				"Hierarchy": func() error { _, err := wrapped.Hierarchy(context.Background()); return err },
			} {
				err := call()
				if !errors.Is(err, tc.err) {
					t.Fatalf("%s returned %v, want the identical error %v", label, err, tc.err)
				}
				var gotExit *device.ProcessExitError
				if tc.is == nil && (!errors.As(err, &gotExit) || gotExit != exit) {
					t.Fatalf("%s: errors.As lost the ProcessExitError", label)
				}
				if tc.is != nil && !errors.Is(err, tc.is) {
					t.Fatalf("%s: errors.Is(%v) is false", label, tc.is)
				}
			}
		})
	}
}

func TestObservingDriver_CancelledContextReachesTheInnerDriver(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	wrapped := session.NewObservingDriver(mockdriver.New(), nil)
	if err := wrapped.Tap(ctx, wire.Point{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Tap with a canceled context = %v, want context.Canceled", err)
	}
}

func orchestraRun(t *testing.T, d driver.Driver) {
	t.Helper()
	ctx := context.Background()
	o := orchestra.New(orchestra.OrchestraDeps{Driver: d, Clock: orchestra.NewFakeClock(0)})
	selector := orchestra.Selector{ID: patternPtr("btn")}
	if _, err := o.Tap(ctx, selector, orchestra.TapOptions{}); err != nil {
		t.Fatalf("Tap: %v", err)
	}
	if _, err := o.Hierarchy(ctx); err != nil {
		t.Fatalf("Hierarchy: %v", err)
	}
	if _, err := o.Screenshot(ctx); err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	if _, err := o.PressKey(ctx, driver.KeyBack); err != nil {
		t.Fatalf("PressKey: %v", err)
	}
}

func patternPtr(s string) *orchestra.Pattern {
	p := orchestra.LiteralPattern(s)
	return &p
}

func stagedMock() *mockdriver.Driver {
	d := mockdriver.New()
	for range 8 {
		d.StageHierarchy(wire.TreeNodeWire{
			Attributes: map[string]string{"id": "root"},
			Children: []wire.TreeNodeWire{{
				Attributes: map[string]string{"id": "btn", "text": "Go", "bounds": "0,0,100,50"},
			}},
		})
	}
	return d
}

func callMethods(d *mockdriver.Driver) []string {
	methods := make([]string, len(d.Calls))
	for i, c := range d.Calls {
		methods[i] = c.Method
	}
	return methods
}

func TestObservingDriver_OrchestraRunMakesTheSameDriverCallsWrappedAndUnwrapped(t *testing.T) {
	bare := stagedMock()
	orchestraRun(t, bare)

	inner := stagedMock()
	observer := &recordingObserver{}
	orchestraRun(t, session.NewObservingDriver(inner, observer))

	if !reflect.DeepEqual(callMethods(bare), callMethods(inner)) {
		t.Fatalf("wrapped calls = %v, unwrapped calls = %v", callMethods(inner), callMethods(bare))
	}
	if len(observer.records) == 0 {
		t.Fatal("the observer saw no capture of the run")
	}
}

func BenchmarkCapture(b *testing.B) {
	ctx := context.Background()
	tree := wire.TreeNodeWire{Attributes: map[string]string{"id": "root"}, Children: make([]wire.TreeNodeWire, 200)}
	prepared := func() *mockdriver.Driver {
		d := mockdriver.New()
		d.StageHierarchyRepeated(tree, 1)
		d.ScreenshotData = make([]byte, 4096)
		return d
	}
	run := func(b *testing.B, d driver.Driver) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := d.Hierarchy(ctx); err != nil {
				b.Fatal(err)
			}
			if _, err := d.Screenshot(ctx); err != nil {
				b.Fatal(err)
			}
			if mock, ok := driver.As[*mockdriver.Driver](d); ok {
				mock.Calls = mock.Calls[:0]
			}
		}
	}
	b.Run("unwrapped", func(b *testing.B) { run(b, prepared()) })
	b.Run("wrapped", func(b *testing.B) { run(b, session.NewObservingDriver(prepared(), nil)) })
}
