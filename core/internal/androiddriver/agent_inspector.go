package androiddriver

import (
	"context"
	"strings"

	"dev.atomyx/internal/process"
)

// AgentInspector reads the state of the Atomyx agent on one Android
// device through adb. It never changes the device.
type AgentInspector struct {
	proc   process.Runner
	serial string
}

// NewAgentInspector returns an AgentInspector for the device with the
// given serial. proc issues every adb command.
func NewAgentInspector(proc process.Runner, serial string) *AgentInspector {
	return &AgentInspector{proc: proc, serial: serial}
}

// Installed reports whether the agent package is installed.
func (i *AgentInspector) Installed(ctx context.Context) (bool, error) {
	result, err := i.proc.Run(ctx, "adb", "-s", i.serial, "shell", "pm", "list", "packages", agentPackage)
	if err != nil {
		return false, err
	}
	return parsePackageListOutput(result.Stdout, agentPackage), nil
}

// AccessibilityServices reports whether the Atomyx accessibility
// service is enabled and which other services are, each as the
// package and class component Android stores.
func (i *AgentInspector) AccessibilityServices(ctx context.Context) (agentEnabled bool, others []string, err error) {
	result, err := i.proc.Run(ctx, "adb", "-s", i.serial, "shell", "settings", "get", "secure", "enabled_accessibility_services")
	if err != nil {
		return false, nil, err
	}
	agentEnabled, others = parseEnabledServices(result.Stdout)
	return agentEnabled, others, nil
}

// parseEnabledServices splits the colon separated value of the enabled
// services setting. Android prints null when the setting is unset. A
// component may spell its class with a leading dot relative to its
// package, so each entry is expanded before it is compared with the
// agent's own service.
func parseEnabledServices(stdout string) (agentEnabled bool, others []string) {
	value := strings.TrimSpace(stdout)
	if value == "" || value == "null" {
		return false, nil
	}
	for _, entry := range strings.Split(value, ":") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if expandComponent(entry) == accessibilityService {
			agentEnabled = true
			continue
		}
		others = append(others, entry)
	}
	return agentEnabled, others
}

func expandComponent(component string) string {
	pkg, class, ok := strings.Cut(component, "/")
	if ok && strings.HasPrefix(class, ".") {
		return pkg + "/" + pkg + class
	}
	return component
}
