package setup

import "context"

// IOSTarget is the kind of iOS device a setup run prepares.
type IOSTarget int

const (
	// IOSTargetUnknown means no iOS device was resolved, so a step
	// checks what a physical device would need.
	IOSTargetUnknown IOSTarget = iota
	// IOSTargetSimulator is an iOS simulator on this machine.
	IOSTargetSimulator
	// IOSTargetPhysical is an iPhone or iPad connected over USB.
	IOSTargetPhysical
)

// IOSIproxyStepID is the stable identifier the step reports.
const IOSIproxyStepID = "ios-iproxy"

type iosIproxyStep struct {
	target IOSTarget
	tools  ToolLocator
}

// NewIOSIproxyStep returns the step that requires iproxy on the host
// search path when the target is a physical device or unknown, because
// iproxy tunnels the runner's port over USB. A simulator needs no
// tunnel, so the step passes for it. It has no fix.
func NewIOSIproxyStep(target IOSTarget, tools ToolLocator) Step {
	return iosIproxyStep{target: target, tools: tools}
}

func (iosIproxyStep) ID() string { return IOSIproxyStepID }

func (s iosIproxyStep) Check(context.Context) Result {
	if s.target == IOSTargetSimulator {
		return Result{Status: StatusOK, Message: "iproxy is not needed for a simulator"}
	}
	if path, found := s.tools.Locate("iproxy"); found {
		return Result{Status: StatusOK, Message: "iproxy is available at " + path}
	}
	return Result{
		Status:  StatusManual,
		Message: "iproxy was not found on PATH",
		Remedy:  "Install it with brew install libusbmuxd, then run atomyx setup again.",
	}
}
