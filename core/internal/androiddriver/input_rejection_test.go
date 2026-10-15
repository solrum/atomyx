package androiddriver_test

import (
	"context"
	"strings"
	"testing"

	"dev.atomyx/internal/androiddriver"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/process"
	"dev.atomyx/internal/transport"
	"dev.atomyx/internal/wire"
)

// scriptedReplyTransport answers the input routes with fixed replies
// so a test controls what the agent reports.
type scriptedReplyTransport struct {
	*fakeTransport
	typeReply     transport.TypeKeyboardResult
	gestureReply  transport.GestureResult
	dispatchReply transport.DispatchGestureResult
}

func (s *scriptedReplyTransport) TypeKeyboard(context.Context, transport.TypeKeyboardRequest) (transport.TypeKeyboardResult, error) {
	return s.typeReply, nil
}

func (s *scriptedReplyTransport) TapCoords(context.Context, transport.TapCoordsRequest) (transport.GestureResult, error) {
	return s.gestureReply, nil
}

func (s *scriptedReplyTransport) LongPress(context.Context, transport.LongPressRequest) (transport.GestureResult, error) {
	return s.gestureReply, nil
}

func (s *scriptedReplyTransport) Swipe(context.Context, transport.SwipeRequest) (transport.GestureResult, error) {
	return s.gestureReply, nil
}

func (s *scriptedReplyTransport) DispatchGesture(context.Context, transport.DispatchGestureRequest) (transport.DispatchGestureResult, error) {
	return s.dispatchReply, nil
}

func newScriptedDriver(s *scriptedReplyTransport) *androiddriver.AndroidDriver {
	s.fakeTransport = newFakeTransport()
	return androiddriver.New(s, process.NewFake(), androiddriver.Options{Serial: "S1"})
}

func requireAgentRejected(t *testing.T, err error, wantInMessage string) {
	t.Helper()
	code, message, remedy := diag.Describe(err)
	if code != diag.CodeAgentRejected {
		t.Fatalf("error = %v, want code %s", err, diag.CodeAgentRejected)
	}
	if !strings.Contains(message, wantInMessage) {
		t.Fatalf("message = %q, want it to contain %q", message, wantInMessage)
	}
	if remedy == "" {
		t.Fatalf("remedy is empty")
	}
}

func TestAndroidDriver_InputText_AgentVerdict(t *testing.T) {
	cases := []struct {
		name    string
		reply   transport.TypeKeyboardResult
		wantMsg string
	}{
		{"success false with reason", transport.TypeKeyboardResult{Success: false, Typed: 0, Total: 9, Reason: "no focused input"}, "no focused input"},
		{"success false without reason", transport.TypeKeyboardResult{Success: false, Typed: 0, Total: 9}, "the text was not entered"},
		{"partial entry", transport.TypeKeyboardResult{Success: true, Typed: 4, Total: 9}, "4 of 9 characters"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &scriptedReplyTransport{typeReply: tc.reply}
			d := newScriptedDriver(s)
			requireAgentRejected(t, d.InputText(context.Background(), "password1"), tc.wantMsg)
		})
	}
}

func TestAndroidDriver_InputText_AcceptsFullAndVerdictlessReplies(t *testing.T) {
	for name, reply := range map[string]transport.TypeKeyboardResult{
		"full entry":      {Success: true, Typed: 9, Total: 9},
		"omitted verdict": {},
	} {
		t.Run(name, func(t *testing.T) {
			s := &scriptedReplyTransport{typeReply: reply}
			d := newScriptedDriver(s)
			if err := d.InputText(context.Background(), "password1"); err != nil {
				t.Fatalf("InputText() error = %v", err)
			}
		})
	}
}

func TestAndroidDriver_Gestures_AgentRefusalSurfacesAsRejection(t *testing.T) {
	reason := "gesture completion not reported within 5000ms"
	code := "gesture_timed_out"
	s := &scriptedReplyTransport{
		gestureReply:  transport.GestureResult{Ok: false, Reason: &reason, Code: &code},
		dispatchReply: transport.DispatchGestureResult{Ok: false, Reason: &reason, Code: &code},
	}
	d := newScriptedDriver(s)
	ctx := context.Background()
	p := wire.Point{X: 1, Y: 2}

	requireAgentRejected(t, d.Tap(ctx, p), "not reported within")
	requireAgentRejected(t, d.LongPress(ctx, p, 800), "not reported within")
	requireAgentRejected(t, d.Swipe(ctx, p, wire.Point{X: 3, Y: 4}, 300), "not reported within")
	_, err := d.DispatchGesture(ctx, driver.Gesture{Pointers: []driver.GesturePointer{{ID: "f1", Waypoints: []driver.GestureWaypoint{
		{Phase: driver.GesturePhaseDown, Point: p},
		{Phase: driver.GesturePhaseUp, Point: p, AtOffsetSeconds: 0.1},
	}}}})
	requireAgentRejected(t, err, "not reported within")
}
