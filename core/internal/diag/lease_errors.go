package diag

import "fmt"

// NewDeviceHeld builds the CodeDeviceHeld error for a device whose
// lease another Atomyx process holds. holder names that process, for
// example "pid 4242 (atomyx run), running". It is the operator-facing
// form, so its remedy tells a person to stop the holder.
func NewDeviceHeld(holder string) error {
	return New(
		CodeDeviceHeld,
		fmt.Sprintf("the device is in use by another Atomyx process: %s", holder),
		"Stop that process or wait for it to finish, then run the command again. "+
			"Only one Atomyx process can drive a device at a time.",
		nil,
	)
}

// NewDeviceReleasedHeld builds the CodeDeviceReleasedHeld error for an
// agent session that released its device and finds it held by another
// process. holder names that process, for example "atomyx run, running".
// Its remedy is addressed to an agent.
func NewDeviceReleasedHeld(holder string) error {
	return New(
		CodeDeviceReleasedHeld,
		fmt.Sprintf("this session released the device and another Atomyx process now holds it: %s", holder),
		"Retry the call shortly, because the other process may finish soon. "+
			"If it keeps failing, ask the operator to release the device.",
		nil,
	)
}

// NewHolderBusy builds the CodeHolderBusy error for a request that finds
// the device holder in the middle of other work. work names that work,
// for example "a tool call is running".
func NewHolderBusy(work string) error {
	return New(
		CodeHolderBusy,
		fmt.Sprintf("the device holder is busy: %s", work),
		"Wait for that work to finish, then ask again.",
		nil,
	)
}

// NewRunExpired builds the CodeRunExpired error for a tool call on a
// run that the open-run idle timeout already closed.
func NewRunExpired(runID string) error {
	return New(
		CodeRunExpired,
		fmt.Sprintf("run %s expired after being idle and was closed as cancelled", runID),
		"Start a new run, then repeat the work that was lost.",
		nil,
	)
}
