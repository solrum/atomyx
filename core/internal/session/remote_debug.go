package session

import (
	"context"
	"slices"

	"dev.atomyx/internal/attach"
	"dev.atomyx/internal/diag"
)

// DebugClient is the viewer-side control of a paused run on a remote
// holder. Every method is a request to the holder, which decides whether
// the run is in a state that admits it: Variables, Resume and Act fail
// with diag.CodeHolderBusy unless the holder is paused.
type DebugClient interface {
	// SetBreakpoints replaces every breakpoint of the run with locations.
	SetBreakpoints(ctx context.Context, locations []string) error
	// Pause asks the run to stop before its next step.
	Pause(ctx context.Context) error
	// Resume lets the paused run continue, or stop after the next step
	// when opts.Step is set. A result with Resumed false carries the
	// advisory MissReason, and repeating the call with opts.Force set
	// overrides it.
	Resume(ctx context.Context, opts ResumeOptions) (ResumeResult, error)
	// Act runs one step written in script YAML while the run is paused.
	Act(ctx context.Context, stepYAML string) (ActResult, error)
	// Variables reports the redacted variables at the pause.
	Variables(ctx context.Context) (VariablesView, error)
}

// Debug returns the handle's debug client, or false when the holder did
// not advertise the debug feature on the handle's connection.
func (h *RemoteHandle) Debug() (DebugClient, bool) {
	if !slices.Contains(h.currentClient().Features(), attach.FeatureDebug) {
		return nil, false
	}
	return remoteDebug{h: h}, true
}

type remoteDebug struct {
	h *RemoteHandle
}

var _ DebugClient = remoteDebug{}

func (d remoteDebug) call(ctx context.Context, messageType string, data, result any) error {
	client := d.h.currentClient()
	if !slices.Contains(client.Features(), attach.FeatureDebug) {
		return diag.NewUnsupported(messageType)
	}
	return client.Call(ctx, messageType, data, result)
}

func (d remoteDebug) SetBreakpoints(ctx context.Context, locations []string) error {
	if locations == nil {
		locations = []string{}
	}
	return d.call(ctx, attach.TypeDebugBreakpointsSet, attach.DebugBreakpoints{Locations: locations}, nil)
}

func (d remoteDebug) Pause(ctx context.Context) error {
	return d.call(ctx, attach.TypeDebugPause, nil, nil)
}

func (d remoteDebug) Resume(ctx context.Context, opts ResumeOptions) (ResumeResult, error) {
	messageType := attach.TypeDebugResume
	if opts.Step {
		messageType = attach.TypeDebugStep
	}
	var result attach.DebugResumeResult
	if err := d.call(ctx, messageType, attach.DebugResume{Force: opts.Force}, &result); err != nil {
		return ResumeResult{}, err
	}
	return ResumeResult{Resumed: result.Resumed, MissReason: result.MissReason}, nil
}

func (d remoteDebug) Act(ctx context.Context, stepYAML string) (ActResult, error) {
	var result attach.ActResult
	if err := d.call(ctx, attach.TypeActStep, attach.ActStep{Step: stepYAML}, &result); err != nil {
		return ActResult{}, err
	}
	return ActResult{
		Command:    result.Command,
		Outcome:    result.Outcome,
		Message:    result.Message,
		DurationMs: result.DurationMs,
		Artifacts:  result.Artifacts,
	}, nil
}

func (d remoteDebug) Variables(ctx context.Context) (VariablesView, error) {
	var result attach.DebugVariables
	if err := d.call(ctx, attach.TypeDebugVariablesGet, nil, &result); err != nil {
		return VariablesView{}, err
	}
	return VariablesView{Variables: result.Variables}, nil
}
