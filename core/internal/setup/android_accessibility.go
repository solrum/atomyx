package setup

import (
	"context"
	"strings"
)

// AccessibilityState lists the accessibility services enabled on a
// device.
type AccessibilityState struct {
	// AgentEnabled is true when the Atomyx accessibility service is
	// among the enabled services.
	AgentEnabled bool
	// Others are the enabled services that are not Atomyx's.
	Others []string
}

// AccessibilityControl reads and changes the accessibility services
// enabled on an Android device.
type AccessibilityControl interface {
	// AccessibilityServices reports the enabled accessibility
	// services.
	AccessibilityServices(ctx context.Context, serial string) (AccessibilityState, error)
	// EnableAgentAccessibility enables the Atomyx accessibility
	// service. Android keeps one list of enabled services, so every
	// other enabled service is disabled by this call.
	EnableAgentAccessibility(ctx context.Context, serial string) error
}

const androidAccessibilityStepID = "android-accessibility"

type androidAccessibilityStep struct {
	serial  string
	control AccessibilityControl
}

// NewAndroidAccessibilityStep returns the step that requires the
// Atomyx accessibility service to be enabled. Its fix enables it and
// states which other services that disabled.
func NewAndroidAccessibilityStep(serial string, control AccessibilityControl) Fixer {
	return androidAccessibilityStep{serial: serial, control: control}
}

func (androidAccessibilityStep) ID() string { return androidAccessibilityStepID }

func (s androidAccessibilityStep) Check(ctx context.Context) Result {
	state, err := s.control.AccessibilityServices(ctx, s.serial)
	if err != nil {
		return failedFrom("could not read the enabled accessibility services", err,
			"Check that the device is connected and unlocked, then run atomyx setup again.")
	}
	if state.AgentEnabled {
		return Result{Status: StatusOK, Message: "the Atomyx accessibility service is enabled"}
	}
	return Result{
		Status:  StatusManual,
		Message: "the Atomyx accessibility service is not enabled. " + disableWarning(state.Others),
		Remedy:  "Run atomyx setup without --check to enable it, or enable Atomyx in Settings, Accessibility on the device.",
	}
}

func (s androidAccessibilityStep) Fix(ctx context.Context) Result {
	before, err := s.control.AccessibilityServices(ctx, s.serial)
	if err != nil {
		return failedFrom("could not read the enabled accessibility services", err,
			"Check that the device is connected and unlocked, then run atomyx setup again.")
	}
	if err := s.control.EnableAgentAccessibility(ctx, s.serial); err != nil {
		return failedFrom("could not enable the Atomyx accessibility service", err,
			"Enable Atomyx in Settings, Accessibility on the device, then run atomyx setup again.")
	}
	message := "enabled the Atomyx accessibility service"
	if len(before.Others) > 0 {
		message += ". These services were enabled before and are now disabled: " + strings.Join(before.Others, ", ")
	}
	return Result{Status: StatusFixed, Message: message}
}

func disableWarning(others []string) string {
	if len(others) == 0 {
		return "Enabling it disables any other enabled accessibility service."
	}
	return "Enabling it disables the other enabled accessibility services: " + strings.Join(others, ", ") + "."
}
