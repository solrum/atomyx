package setup

import (
	"context"
	"strings"
)

// DeveloperDirReader reads the developer directory the active Xcode
// toolchain is selected from.
type DeveloperDirReader interface {
	// DeveloperDir returns the selected developer directory, or an
	// error when none is selected.
	DeveloperDir(ctx context.Context) (string, error)
}

// IOSXcodeStepID is the stable identifier the step reports.
const IOSXcodeStepID = "ios-xcode"

type iosXcodeStep struct {
	tools ToolLocator
	dir   DeveloperDirReader
}

// NewIOSXcodeStep returns the step that requires xcrun on the host
// search path and a selected developer directory inside a full Xcode
// installation, because the command line tools alone cannot build the
// iOS agent. It has no fix,
// because installing the toolchain is the operator's decision.
func NewIOSXcodeStep(tools ToolLocator, dir DeveloperDirReader) Step {
	return iosXcodeStep{tools: tools, dir: dir}
}

func (iosXcodeStep) ID() string { return IOSXcodeStepID }

func (s iosXcodeStep) Check(ctx context.Context) Result {
	if _, found := s.tools.Locate("xcrun"); !found {
		return Result{
			Status:  StatusManual,
			Message: "xcrun was not found on PATH",
			Remedy:  "Install the Xcode command line tools with xcode-select --install, or install Xcode from the App Store, then run atomyx setup again.",
		}
	}
	dir, err := s.dir.DeveloperDir(ctx)
	if err != nil || strings.TrimSpace(dir) == "" {
		return Result{
			Status:  StatusManual,
			Message: "no Xcode developer directory is selected",
			Remedy:  "Install Xcode, then select it with sudo xcode-select -s /Applications/Xcode.app/Contents/Developer, or run xcode-select --install for the command line tools, then run atomyx setup again.",
		}
	}
	dir = strings.TrimSpace(dir)
	if !insideAppBundle(dir) {
		return Result{
			Status:  StatusManual,
			Message: "the developer directory " + dir + " holds only the command line tools, which cannot build or run the iOS agent",
			Remedy:  "Install Xcode from the App Store, then select it with sudo xcode-select -s /Applications/Xcode.app/Contents/Developer and run atomyx setup again.",
		}
	}
	return Result{Status: StatusOK, Message: "xcrun is available and the developer directory is " + dir}
}

// insideAppBundle reports whether dir sits inside an application
// bundle, which is where a full Xcode keeps its developer directory.
// The command line tools live outside any bundle.
func insideAppBundle(dir string) bool {
	for _, segment := range strings.Split(dir, "/") {
		if strings.HasSuffix(segment, ".app") {
			return true
		}
	}
	return false
}
