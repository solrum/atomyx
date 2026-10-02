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
	}, drivertest.Hooks{
		FailPing: func() {
			inner.FailNextPingForContract(diag.New(diag.CodeAgentUnreachable, "drivertest: simulated unreachable agent", "Confirm the agent is running on the device, then try again.", nil))
		},
	})
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
	}, drivertest.Hooks{
		FailPing: func() {
			inner.FailNextPingForContract(diag.New(diag.CodeAgentUnreachable, "drivertest: simulated unreachable agent", "Confirm the agent is running on the device, then try again.", nil))
		},
	})
}
