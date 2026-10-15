package androiddriver

import (
	"context"

	"dev.atomyx/internal/transport"
)

// Transport is the narrow slice of transport.HTTPClient AndroidDriver
// depends on. Declaring it here, rather than depending on
// *transport.HTTPClient directly, lets a test substitute a fake that
// implements only the routes this adapter calls.
type Transport interface {
	Health(ctx context.Context) (transport.HealthResponse, error)
	Ping(ctx context.Context) (transport.PingResponse, error)
	Tree(ctx context.Context) (transport.AndroidTreeNode, error)
	TapCoords(ctx context.Context, req transport.TapCoordsRequest) (transport.GestureResult, error)
	LongPress(ctx context.Context, req transport.LongPressRequest) (transport.GestureResult, error)
	Swipe(ctx context.Context, req transport.SwipeRequest) (transport.GestureResult, error)
	DispatchGesture(ctx context.Context, req transport.DispatchGestureRequest) (transport.DispatchGestureResult, error)
	TypeKeyboard(ctx context.Context, req transport.TypeKeyboardRequest) (transport.TypeKeyboardResult, error)
	ClearFocusedInput(ctx context.Context) (transport.ClearFocusedInputResult, error)
	HideKeyboard(ctx context.Context) (transport.AckResult, error)
	PressKey(ctx context.Context, req transport.KeyRequest) (transport.OkResponse, error)
	LaunchApp(ctx context.Context, req transport.LaunchRequest) (transport.OkResponse, error)
	CurrentActivity(ctx context.Context) (transport.CurrentActivityResponse, error)
	Apps(ctx context.Context) ([]transport.InstalledApp, error)
	Screenshot(ctx context.Context) (transport.ScreenshotResponse, error)
}

var _ Transport = (*transport.HTTPClient)(nil)
