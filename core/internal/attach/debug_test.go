package attach

import (
	"context"
	"slices"
	"strings"
	"testing"

	"dev.atomyx/internal/diag"
)

type fakeDebug struct {
	locations []string
	pauses    int
	steps     []bool
	forces    []bool
	acts      []string
	err       error
}

func (f *fakeDebug) SetBreakpoints(locations []string) error {
	f.locations = locations
	return f.err
}

func (f *fakeDebug) Pause() error {
	f.pauses++
	return f.err
}

func (f *fakeDebug) Resume(_ context.Context, step, force bool) (DebugResumeResult, error) {
	f.steps, f.forces = append(f.steps, step), append(f.forces, force)
	return DebugResumeResult{Resumed: true}, f.err
}

func (f *fakeDebug) Variables() (DebugVariables, error) {
	return DebugVariables{Variables: map[string]string{"user": "ann"}}, f.err
}

func (f *fakeDebug) Act(_ context.Context, stepYAML string) (ActResult, error) {
	f.acts = append(f.acts, stepYAML)
	return ActResult{Command: "tap", Outcome: "passed"}, f.err
}

var debugRequests = []struct {
	messageType string
	data        any
}{
	{TypeDebugBreakpointsSet, DebugBreakpoints{Locations: []string{"a.yml:3"}}},
	{TypeDebugPause, nil},
	{TypeDebugResume, DebugResume{}},
	{TypeDebugStep, DebugResume{}},
	{TypeDebugVariablesGet, nil},
	{TypeActStep, ActStep{Step: "- tap: Login"}},
}

func TestAHolderWithoutADebugHandlerAdvertisesNoDebugAndAnswersUnsupported(t *testing.T) {
	srv := startServer(t, ServerConfig{Handlers: Handlers{Session: fakeSession{Status{State: "paused"}}}}, nil)
	client := dialRaw(t, srv.Path())
	if slices.Contains(helloFeatures(t, client), FeatureDebug) {
		t.Fatal("hello advertises debug without a debug handler")
	}
	for _, r := range debugRequests {
		if got := failureCode(t, client.call(r.messageType, r.data)); got != diag.CodeUnsupported {
			t.Fatalf("%s: code = %q, want %s", r.messageType, got, diag.CodeUnsupported)
		}
	}
}

func TestADebugHandlerIsAdvertisedAndEachRequestReachesIt(t *testing.T) {
	h := &fakeDebug{}
	srv := startServer(t, ServerConfig{Handlers: Handlers{Debug: h}}, nil)
	client := dialRaw(t, srv.Path())
	if !slices.Contains(helloFeatures(t, client), FeatureDebug) {
		t.Fatal("hello lacks debug although a debug handler is set")
	}
	for _, r := range debugRequests {
		if reply := client.call(r.messageType, r.data); reply.Ok == nil || !*reply.Ok {
			t.Fatalf("%s failed: %+v", r.messageType, reply)
		}
	}
	if !slices.Equal(h.locations, []string{"a.yml:3"}) || h.pauses != 1 || !slices.Equal(h.acts, []string{"- tap: Login"}) {
		t.Fatalf("handler saw locations %v, pauses %d, acts %v", h.locations, h.pauses, h.acts)
	}
	if !slices.Equal(h.steps, []bool{false, true}) {
		t.Fatalf("step flags = %v, want resume then step", h.steps)
	}
}

func TestResumeForwardsTheForceFlag(t *testing.T) {
	h := &fakeDebug{}
	srv := startServer(t, ServerConfig{Handlers: Handlers{Debug: h}}, nil)
	client := dialRaw(t, srv.Path())
	client.hello()
	client.call(TypeDebugResume, DebugResume{Force: true})
	if !slices.Equal(h.forces, []bool{true}) {
		t.Fatalf("force flags = %v, want [true]", h.forces)
	}
}

func TestADebugRefusalIsTheResponseAndTheConnectionStaysUsable(t *testing.T) {
	h := &fakeDebug{err: diag.NewHolderBusy("the run is not paused")}
	srv := startServer(t, ServerConfig{Handlers: Handlers{Debug: h}}, nil)
	client := dialRaw(t, srv.Path())
	client.hello()
	if got := failureCode(t, client.call(TypeDebugVariablesGet, nil)); got != diag.CodeHolderBusy {
		t.Fatalf("code = %q, want %s", got, diag.CodeHolderBusy)
	}
	if ok := client.call(TypeDebugPause, nil).Ok; ok == nil || *ok {
		t.Fatal("the refusal was not forwarded for the next request")
	}
}

func TestInvalidDebugRequestsAreRefusedBeforeTheHandler(t *testing.T) {
	h := &fakeDebug{}
	srv := startServer(t, ServerConfig{Handlers: Handlers{Debug: h}}, nil)
	client := dialRaw(t, srv.Path())
	client.hello()
	tooMany := make([]string, MaxBreakpoints+1)
	cases := []struct {
		messageType string
		data        any
	}{
		{TypeActStep, ActStep{}},
		{TypeActStep, ActStep{Step: strings.Repeat("x", MaxActStepBytes+1)}},
		{TypeDebugBreakpointsSet, DebugBreakpoints{Locations: tooMany}},
	}
	for _, c := range cases {
		if got := failureCode(t, client.call(c.messageType, c.data)); got != diag.CodeInvalidArgument {
			t.Fatalf("%s: code = %q, want %s", c.messageType, got, diag.CodeInvalidArgument)
		}
	}
	if len(h.acts) != 0 || h.locations != nil {
		t.Fatal("the handler saw an invalid request")
	}
}
