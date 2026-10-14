package setup

// AndroidAgentHost is everything the Android agent steps ask of the
// device tooling, gathered so a composition root supplies one value
// for all of them.
type AndroidAgentHost interface {
	APKResolver
	AgentInstaller
	AgentReinstaller
	AccessibilityControl
	AgentHealthProbe
	AgentStarter
	AgentProtocolProbe
}
