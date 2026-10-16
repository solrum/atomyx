package runner

// DefaultCommands returns the built-in command registry. Adding a
// new command means creating a new `<name>_command.go` file and
// appending its CommandDefinition here, and no other file needs to
// change.
func DefaultCommands() []CommandDefinition {
	return []CommandDefinition{
		LaunchAppCommand{},
		TapCommand{},
		TypeCommand{},
		WaitForCommand{},
		AssertVisibleCommand{},
		AssertNotVisibleCommand{},
		AssertExistsCommand{},
		ScreenshotCommand{},
		SwipeCommand{},
		PressKeyCommand{},
		BackCommand{},
		SleepCommand{},
		CaptureCommand{},
		AssertApiCommand{},
		ExtractCommand{},
		ReadCommand{},
		AssertExprCommand{},
		HandleCommand{},
		BranchCommand{},
		RunFlowCommand{},
		PointerCommand{},
		LongPressCommand{},
		PinchCommand{},
		RotateCommand{},
		RepeatCommand{},
		RetryCommand{},
		StopAppCommand{},
		KillAppCommand{},
		ClearStateCommand{},
		OpenLinkCommand{},
		SetPermissionsCommand{},
		SetOrientationCommand{},
		SetLocationCommand{},
		TravelCommand{},
		HideKeyboardCommand{},
		EraseTextCommand{},
		ScrollUntilVisibleCommand{},
		WaitForAnimationToEndCommand{},
		InputRandomCommand{},
	}
}
