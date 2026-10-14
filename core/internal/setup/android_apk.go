package setup

import "context"

// APKResolver locates the Android agent APK the way the agent
// launcher does.
type APKResolver interface {
	// ResolveAgentAPK reports the agent APK path and whether a file
	// exists there.
	ResolveAgentAPK() (path string, found bool)
}

// AndroidAPKStepID is the stable identifier the step reports.
const AndroidAPKStepID = "android-apk"

type androidAPKStep struct{ resolver APKResolver }

// NewAndroidAPKStep returns the step that requires a built agent APK.
// It has no fix, because building the APK needs the Android toolchain.
func NewAndroidAPKStep(resolver APKResolver) Step {
	return androidAPKStep{resolver: resolver}
}

func (androidAPKStep) ID() string { return AndroidAPKStepID }

func (s androidAPKStep) Check(context.Context) Result {
	path, found := s.resolver.ResolveAgentAPK()
	if found {
		return Result{Status: StatusOK, Message: "agent APK found at " + path}
	}
	return Result{
		Status:  StatusManual,
		Message: "agent APK not found at " + path,
		Remedy:  "Build it with cd platforms/android-agent && ./gradlew :app:assembleDebug, or set ATOMYX_ANDROID_APK to the path of a built agent APK.",
	}
}
