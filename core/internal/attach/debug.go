package attach

import (
	"context"
	"fmt"
)

// MaxBreakpoints bounds the locations of one debug.breakpoints.set
// request. A longer list is refused with diag.CodeInvalidArgument.
const MaxBreakpoints = 256

// MaxActStepBytes bounds the YAML text of one act.step request. A longer
// text is refused with diag.CodeInvalidArgument.
const MaxActStepBytes = 16 * 1024

// DebugHandler answers the debug group and act.step. A holder whose run
// host can pause its script supplies one.
type DebugHandler interface {
	// SetBreakpoints replaces every breakpoint with locations. The last
	// writer wins.
	SetBreakpoints(locations []string) error
	// Pause asks the run to stop before its next step.
	Pause() error
	// Resume lets the paused run continue, or stop again after the next
	// step when step is set. force skips the advisory check of the next
	// step.
	Resume(ctx context.Context, step, force bool) (DebugResumeResult, error)
	// Variables returns the redacted variables at the pause.
	Variables() (DebugVariables, error)
	// Act runs one step written in script YAML while the run is paused.
	Act(ctx context.Context, stepYAML string) (ActResult, error)
}

// debug answers a request of the debug group or act.step.
func (c *conn) debug(ctx context.Context, h DebugHandler, f Frame) bool {
	switch f.Type {
	case TypeDebugBreakpointsSet:
		var request DebugBreakpoints
		if err := f.Decode(&request); err != nil {
			return c.fail(f, invalidFrame(err.Error()))
		}
		if len(request.Locations) > MaxBreakpoints {
			return c.fail(f, invalidFrame(fmt.Sprintf("more than %d breakpoints", MaxBreakpoints)))
		}
		return c.answer(f, nil, h.SetBreakpoints(request.Locations))
	case TypeDebugPause:
		return c.answer(f, nil, h.Pause())
	case TypeDebugResume, TypeDebugStep:
		var request DebugResume
		if err := f.Decode(&request); err != nil {
			return c.fail(f, invalidFrame(err.Error()))
		}
		result, err := h.Resume(ctx, f.Type == TypeDebugStep, request.Force)
		return c.answer(f, result, err)
	case TypeDebugVariablesGet:
		result, err := h.Variables()
		return c.answer(f, result, err)
	default:
		var request ActStep
		if err := f.Decode(&request); err != nil {
			return c.fail(f, invalidFrame(err.Error()))
		}
		if request.Step == "" {
			return c.fail(f, invalidFrame("act.step needs a step"))
		}
		if len(request.Step) > MaxActStepBytes {
			return c.fail(f, invalidFrame(fmt.Sprintf("the step is longer than %d bytes", MaxActStepBytes)))
		}
		result, err := h.Act(withClient(ctx, c.client), request.Step)
		return c.answer(f, result, err)
	}
}

// answer sends data as the response, or err as the failure.
func (c *conn) answer(f Frame, data any, err error) bool {
	if err != nil {
		return c.fail(f, err)
	}
	return c.respond(f, data)
}
