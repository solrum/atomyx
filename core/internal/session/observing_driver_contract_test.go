package session_test

import (
	"context"
	"testing"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/driver/drivertest"
	mockdriver "dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/session"
	"dev.atomyx/internal/wire"
)

// stagedObserved exposes the inner mock's contract staging hook, which
// the decorator itself does not forward because it is test-only.
type stagedObserved struct {
	session.ObservedDriver
	inner *mockdriver.Driver
}

func (s stagedObserved) StageHierarchyForContract(tree wire.TreeNodeWire) {
	s.inner.StageHierarchyForContract(tree)
}

func TestObservingDriver_SatisfiesTheDriverContract_OverMock(t *testing.T) {
	var inner *mockdriver.Driver
	drivertest.Run(t, func() driver.Driver {
		inner = mockdriver.New()
		return stagedObserved{ObservedDriver: session.NewObservingDriver(inner, nil), inner: inner}
	}, drivertest.WithSyntheticFixtures(drivertest.Hooks{
		FailPing: func() {
			inner.FailNextPingForContract(diag.New(diag.CodeAgentUnreachable, "drivertest: simulated unreachable agent", "Confirm the agent is running on the device, then try again.", nil))
		},
	}))
}

type stagedStreamingObserved struct {
	session.ObservedDriver
	inner *mockdriver.StreamingDriver
}

func (s stagedStreamingObserved) StageHierarchyForContract(tree wire.TreeNodeWire) {
	s.inner.StageHierarchyForContract(tree)
}

func (s stagedStreamingObserved) streaming() driver.StreamingTouchCapable {
	return s.ObservedDriver.(driver.StreamingTouchCapable)
}

func (s stagedStreamingObserved) StreamingTouchDown(ctx context.Context, p wire.Point, id int) error {
	return s.streaming().StreamingTouchDown(ctx, p, id)
}

func (s stagedStreamingObserved) StreamingTouchMove(ctx context.Context, p wire.Point, id int) error {
	return s.streaming().StreamingTouchMove(ctx, p, id)
}

func (s stagedStreamingObserved) StreamingTouchUp(ctx context.Context, p wire.Point, id int) error {
	return s.streaming().StreamingTouchUp(ctx, p, id)
}

func TestObservingDriver_SatisfiesTheDriverContract_OverStreamingMock(t *testing.T) {
	var inner *mockdriver.StreamingDriver
	drivertest.Run(t, func() driver.Driver {
		inner = mockdriver.NewStreamingTouchCapable()
		return stagedStreamingObserved{ObservedDriver: session.NewObservingDriver(inner, nil), inner: inner}
	}, drivertest.WithSyntheticFixtures(drivertest.Hooks{
		FailPing: func() {
			inner.FailNextPingForContract(diag.New(diag.CodeAgentUnreachable, "drivertest: simulated unreachable agent", "Confirm the agent is running on the device, then try again.", nil))
		},
	}))
}

// The contract runs below cover the launched-app carrier combinations
// of the decorator with test doubles. A real iOS adapter cannot stand
// in here: the adapters' protocol fakes are private to their own test
// packages, and a session test that imported an adapter would give the
// session package a dependency on a concrete driver, which the layer
// rules forbid. The adapters run the same contract unwrapped in their
// own packages.

type carrierAccessors interface {
	LaunchedApp() (bundleID, runnerID string)
	RestoreLaunchedApp(ctx context.Context, bundleID, fromRunnerID string) error
}

func carrierOf(t *testing.T, d session.ObservedDriver) carrierAccessors {
	t.Helper()
	carrier, ok := d.(carrierAccessors)
	if !ok {
		t.Fatalf("%T lost the launched-app accessors of its inner driver", d)
	}
	return carrier
}

type stagedCarrierObserved struct {
	session.ObservedDriver
	inner   *carrierDriver
	carrier carrierAccessors
}

func (s stagedCarrierObserved) StageHierarchyForContract(tree wire.TreeNodeWire) {
	s.inner.StageHierarchyForContract(tree)
}

func (s stagedCarrierObserved) LaunchedApp() (string, string) { return s.carrier.LaunchedApp() }

func (s stagedCarrierObserved) RestoreLaunchedApp(ctx context.Context, bundleID, runnerID string) error {
	return s.carrier.RestoreLaunchedApp(ctx, bundleID, runnerID)
}

func TestObservingDriver_SatisfiesTheDriverContract_OverCarrierMock(t *testing.T) {
	var inner *carrierDriver
	drivertest.Run(t, func() driver.Driver {
		inner = &carrierDriver{Driver: mockdriver.New(), bundleID: "com.example", runnerID: "run-1"}
		wrapped := session.NewObservingDriver(inner, nil)
		return stagedCarrierObserved{ObservedDriver: wrapped, inner: inner, carrier: carrierOf(t, wrapped)}
	}, drivertest.WithSyntheticFixtures(drivertest.Hooks{
		FailPing: func() {
			inner.FailNextPingForContract(diag.New(diag.CodeAgentUnreachable, "drivertest: simulated unreachable agent", "Confirm the agent is running on the device, then try again.", nil))
		},
	}))
}

type stagedStreamingCarrierObserved struct {
	stagedStreamingObserved
	carrier carrierAccessors
}

func (s stagedStreamingCarrierObserved) LaunchedApp() (string, string) {
	return s.carrier.LaunchedApp()
}

func (s stagedStreamingCarrierObserved) RestoreLaunchedApp(ctx context.Context, bundleID, runnerID string) error {
	return s.carrier.RestoreLaunchedApp(ctx, bundleID, runnerID)
}

func TestObservingDriver_SatisfiesTheDriverContract_OverStreamingCarrierMock(t *testing.T) {
	var inner *carrierStreamingDriver
	drivertest.Run(t, func() driver.Driver {
		inner = &carrierStreamingDriver{StreamingDriver: mockdriver.NewStreamingTouchCapable()}
		wrapped := session.NewObservingDriver(inner, nil)
		return stagedStreamingCarrierObserved{
			stagedStreamingObserved: stagedStreamingObserved{ObservedDriver: wrapped, inner: inner.StreamingDriver},
			carrier:                 carrierOf(t, wrapped),
		}
	}, drivertest.WithSyntheticFixtures(drivertest.Hooks{
		FailPing: func() {
			inner.FailNextPingForContract(diag.New(diag.CodeAgentUnreachable, "drivertest: simulated unreachable agent", "Confirm the agent is running on the device, then try again.", nil))
		},
	}))
}
