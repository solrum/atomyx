package history

import (
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/runfolder"
)

// Class says whether a failed run is blamed on the test or on the
// infrastructure it ran on.
type Class string

const (
	// ClassTest is a failure the test owns: an assertion, a missing
	// element, a script error, or a request the test got wrong.
	ClassTest Class = "test"
	// ClassInfra is a failure the test did not cause: the device, the
	// on-device agent, the host tooling, or a resource held by another
	// process.
	ClassInfra Class = "infra"
)

// codeClasses decides every diag code. A code absent from this table is
// a test failure by default, and a test fails when diag declares a code
// the table does not list, so adding a code forces a decision here.
var codeClasses = map[string]Class{
	diag.CodeUnknown:                   ClassTest,
	diag.CodeDeviceNotFound:            ClassInfra,
	diag.CodeAgentUnreachable:          ClassInfra,
	diag.CodeAppInstallFailed:          ClassInfra,
	diag.CodeAppUninstallFailed:        ClassInfra,
	diag.CodeAppDataClearFailed:        ClassInfra,
	diag.CodeAppNotInstalled:           ClassTest,
	diag.CodeToolTimeout:               ClassInfra,
	diag.CodeForegroundUnsupported:     ClassTest,
	diag.CodeDeviceBusy:                ClassInfra,
	diag.CodeDeviceHeld:                ClassInfra,
	diag.CodeDeviceReleasedHeld:        ClassInfra,
	diag.CodeDevicePinned:              ClassInfra,
	diag.CodeRunExpired:                ClassInfra,
	diag.CodeHostPortInUse:             ClassInfra,
	diag.CodeInvalidAppID:              ClassTest,
	diag.CodePointerCaptureUnsupported: ClassTest,
	diag.CodeAgentRejected:             ClassTest,
	diag.CodeInvalidArgument:           ClassTest,
	diag.CodeAgentCommunicationFailed:  ClassInfra,
	diag.CodeAgentNotReady:             ClassInfra,
	diag.CodeAgentVersionMismatch:      ClassInfra,
	diag.CodeProxyRequired:             ClassTest,
	diag.CodeRunnerBuildTimeout:        ClassInfra,
	diag.CodeRunUnreadable:             ClassInfra,
	diag.CodeRunReadOnly:               ClassInfra,
	diag.CodeRunBusy:                   ClassInfra,
	diag.CodeHolderBusy:                ClassInfra,
	diag.CodeUnsupported:               ClassTest,
	diag.CodeUnsupportedVersion:        ClassTest,
	diag.CodeRecordingUnsupported:      ClassTest,
}

// Classify decides who a failed run is blamed on.
//
// A run that ended error, deviceLost, or interrupted is an infrastructure
// failure whatever its code. A run that ended failed is classified by
// its diag code, and a failed run with no code or an unlisted code is a
// test failure. The second result is false for an outcome that is not a
// failure, which are passed and cancelled.
func Classify(outcome runfolder.Outcome, code string) (Class, bool) {
	switch outcome {
	case runfolder.OutcomeError, runfolder.OutcomeDeviceLost, runfolder.OutcomeInterrupted:
		return ClassInfra, true
	case runfolder.OutcomeFailed:
		if class, ok := codeClasses[code]; ok {
			return class, true
		}
		return ClassTest, true
	}
	return "", false
}
