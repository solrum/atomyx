package session_test

import (
	"context"
	"errors"
	"testing"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/driver"
	mockdriver "dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/session"
)

func debuggableMock(t *testing.T, dbg session.Debugger) (*session.Session, *mockdriver.Driver) {
	t.Helper()
	s, err := session.Open(context.Background(), session.Deps{Factory: device.NewMockDriverFactory()}, session.Request{Descriptor: androidDesc("A"), Debugger: dbg})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	mock, ok := driver.As[*mockdriver.Driver](s.Built().Driver)
	if !ok {
		t.Fatal("the built driver does not reach the mock")
	}
	mock.ScreenshotData = encodedPNG(t, 430, 932)
	return s, mock
}

func TestPauseCapture_EnteringAPauseLeavesTheHubWithTheCurrentScreenAndAScreenshot(t *testing.T) {
	s, mock := debuggableMock(t, &fakeDebugger{})
	mock.StageHierarchy(rootTree("at-pause"))

	s.EnterPause(testPause)

	snap := s.Latest()
	if !snap.HasTree || snap.Tree.Attributes["id"] != "at-pause" {
		t.Fatalf("tree after pause entry = %+v, want the screen at the pause", snap.Tree)
	}
	if !snap.HasScreenshot {
		t.Fatal("no screenshot after pause entry")
	}
}

func TestPauseCapture_AnActLeavesTheHubWithThePostActTreeAndAnAdvancedRevision(t *testing.T) {
	for _, step := range []string{"tap: {id: ok}", "back"} {
		t.Run(step, func(t *testing.T) {
			s, mock := debuggableMock(t, &fakeDebugger{})
			mock.StageHierarchy(rootTree("before"))
			mock.StageHierarchy(rootTree("after"))
			s.EnterPause(testPause)
			before := s.Latest()

			if _, err := s.Act(context.Background(), step); err != nil {
				t.Fatal(err)
			}

			after := s.Latest()
			if after.Tree.Attributes["id"] != "after" || after.TreeRevision <= before.TreeRevision {
				t.Fatalf("tree after act = %q at revision %d, want after at a revision above %d", after.Tree.Attributes["id"], after.TreeRevision, before.TreeRevision)
			}
			if !after.HasScreenshot || after.ScreenshotRevision <= before.ScreenshotRevision {
				t.Fatalf("screenshot revision %d after act, want above %d", after.ScreenshotRevision, before.ScreenshotRevision)
			}
		})
	}
}

func TestPauseCapture_AFailedCaptureNeverChangesTheActResult(t *testing.T) {
	s, mock := debuggableMock(t, &fakeDebugger{})
	mock.StageHierarchy(rootTree("before"))
	mock.StageHierarchy(rootTree("after"))
	s.EnterPause(testPause)
	mock.ScreenshotErr = errors.New("device gone")

	res, err := s.Act(context.Background(), "tap: {id: ok}")
	if err != nil {
		t.Fatalf("act error = %v, want the result kept", err)
	}
	if res.Command != "tap" || res.Outcome != "passed" {
		t.Fatalf("act result = %+v, want the debugger's result untouched", res)
	}
	if _, paused := s.Paused(); !paused {
		t.Fatal("a failed capture cleared the pause")
	}
}
