package session

import (
	"context"

	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/wire"
)

// streamingForwarder forwards driver.StreamingTouchCapable. Every
// streaming phase mutates the screen, so each advances the epoch.
type streamingForwarder struct {
	core  *observingCore
	inner driver.StreamingTouchCapable
}

func (f streamingForwarder) StreamingTouchDown(ctx context.Context, point wire.Point, id int) error {
	defer f.core.mutate()()
	return f.inner.StreamingTouchDown(ctx, point, id)
}

func (f streamingForwarder) StreamingTouchMove(ctx context.Context, point wire.Point, id int) error {
	defer f.core.mutate()()
	return f.inner.StreamingTouchMove(ctx, point, id)
}

func (f streamingForwarder) StreamingTouchUp(ctx context.Context, point wire.Point, id int) error {
	defer f.core.mutate()()
	return f.inner.StreamingTouchUp(ctx, point, id)
}

// carrierForwarder forwards launchedAppCarrier. Reading the tracked app
// is passive, and restoring it may launch the app, so that advances the
// epoch.
type carrierForwarder struct {
	core  *observingCore
	inner launchedAppCarrier
}

func (f carrierForwarder) LaunchedApp() (bundleID, runnerID string) { return f.inner.LaunchedApp() }

func (f carrierForwarder) RestoreLaunchedApp(ctx context.Context, bundleID, fromRunnerID string) error {
	defer f.core.mutate()()
	return f.inner.RestoreLaunchedApp(ctx, bundleID, fromRunnerID)
}

type observedStreaming struct {
	*observingCore
	streamingForwarder
}

type observedCarrier struct {
	*observingCore
	carrierForwarder
}

type observedStreamingCarrier struct {
	*observingCore
	streamingForwarder
	carrierForwarder
}
