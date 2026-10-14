package setup

import "context"

// ToolLocator finds a command line tool on the host search path.
type ToolLocator interface {
	// Locate reports the path of the named tool and whether it was
	// found.
	Locate(name string) (path string, found bool)
}

const androidAdbStepID = "android-adb"

type androidAdbStep struct{ tools ToolLocator }

// NewAndroidAdbStep returns the step that requires adb on the host
// search path. It has no fix, because installing platform tools is
// the operator's decision.
func NewAndroidAdbStep(tools ToolLocator) Step {
	return androidAdbStep{tools: tools}
}

func (androidAdbStep) ID() string { return androidAdbStepID }

func (s androidAdbStep) Check(context.Context) Result {
	if path, found := s.tools.Locate("adb"); found {
		return Result{Status: StatusOK, Message: "adb is available at " + path}
	}
	return Result{
		Status:  StatusManual,
		Message: "adb was not found on PATH",
		Remedy:  "Install the Android platform tools, for example with brew install android-platform-tools, make sure adb is on PATH, then run atomyx setup again.",
	}
}
