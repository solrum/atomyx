package setup

import (
	"context"
	"strings"
)

// AccessibilityControl reads and changes the accessibility services
// enabled on an Android device.
type AccessibilityControl interface {
	// AccessibilityServices reports whether the Atomyx accessibility
	// service is enabled and which other services are, each as the
	// component Android stores.
	AccessibilityServices(ctx context.Context, serial string) (agentEnabled bool, others []string, err error)
	// EnableAgentAccessibility enables the Atomyx accessibility
	// service. Android keeps one list of enabled services, so every
	// other enabled service is disabled by this call.
	EnableAgentAccessibility(ctx context.Context, serial string) error
}

// AndroidAccessibilityStepID is the stable identifier the step reports.
const AndroidAccessibilityStepID = "android-accessibility"

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

func (androidAccessibilityStep) ID() string { return AndroidAccessibilityStepID }

func (s androidAccessibilityStep) Check(ctx context.Context) Result {
	agentEnabled, others, err := s.control.AccessibilityServices(ctx, s.serial)
	if err != nil {
		return failedFrom("could not read the enabled accessibility services", err,
			"Check that the device is connected and unlocked, then run atomyx setup again.")
	}
	if agentEnabled {
		return Result{Status: StatusOK, Message: "the Atomyx accessibility service is enabled"}
	}
	return Result{
		Status:  StatusManual,
		Message: "the Atomyx accessibility service is not enabled. " + disableWarning(others),
		Remedy:  "Run atomyx setup without --check to enable it, or enable Atomyx in Settings, Accessibility on the device.",
	}
}

func (s androidAccessibilityStep) Fix(ctx context.Context) Result {
	_, before, err := s.control.AccessibilityServices(ctx, s.serial)
	if err != nil {
		return failedFrom("could not read the enabled accessibility services", err,
			"Check that the device is connected and unlocked, then run atomyx setup again.")
	}
	if err := s.control.EnableAgentAccessibility(ctx, s.serial); err != nil {
		return failedFrom("could not enable the Atomyx accessibility service", err,
			"Enable Atomyx in Settings, Accessibility on the device, then run atomyx setup again.")
	}
	message := "enabled the Atomyx accessibility service"
	if len(before) > 0 {
		message += ". These services were enabled before and are now disabled: " + strings.Join(before, ", ")
	}
	return Result{Status: StatusFixed, Message: message}
}

func disableWarning(others []string) string {
	if len(others) == 0 {
		return "Enabling it disables any other enabled accessibility service."
	}
	return "Enabling it disables the other enabled accessibility services: " + strings.Join(others, ", ") + "."
}
