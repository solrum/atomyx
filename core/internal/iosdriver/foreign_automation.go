package iosdriver

import (
	"context"
	"encoding/json"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"dev.atomyx/internal/driver"
)

const (
	// RunnerAppName is the product name of the Atomyx XCUITest runner
	// app, without the .app suffix.
	RunnerAppName = "AtomyxDriverAgent-Runner"

	foreignKindXCUITest = "XCUITest runner"
	foreignKindWDA      = "WebDriverAgent"

	// simulatorProbeTimeout and deviceProbeTimeout bound each foreign
	// automation probe on its own, so a slow listing never delays
	// Connect for long. devicectl starts a CoreDevice session per call,
	// which makes it the slower of the two.
	simulatorProbeTimeout = 3 * time.Second
	deviceProbeTimeout    = 6 * time.Second
)

// simulatorRunnerLabelPattern captures the bundle id inside a
// "UIKitApplication:<bundleId>[...]" launchd label.
var simulatorRunnerLabelPattern = regexp.MustCompile(`^UIKitApplication:([^\[\s]+)`)

// ownRunnerIdentity names the Atomyx runner so a probe never reports
// it as foreign.
type ownRunnerIdentity struct {
	bundleID string
	appName  string
}

// ForeignAutomation returns the foreign UI automation sessions found
// when Connect last probed the target. It never contacts the device.
func (d *IosDriver) ForeignAutomation() []driver.ForeignAutomationSession {
	d.foreignMu.Lock()
	defer d.foreignMu.Unlock()
	if len(d.foreign) == 0 {
		return nil
	}
	return append([]driver.ForeignAutomationSession(nil), d.foreign...)
}

var _ driver.ForeignAutomationReporter = (*IosDriver)(nil)

// probeForeignAutomation lists the automation sessions on the target
// and caches the result. The probe is best effort: a command or
// parse failure caches nothing and is not reported.
func (d *IosDriver) probeForeignAutomation(ctx context.Context) {
	own := ownRunnerIdentity{bundleID: d.opts.OwnRunnerBundleID, appName: d.opts.OwnRunnerAppName}
	var sessions []driver.ForeignAutomationSession
	if d.opts.Kind == KindSimulator {
		probeCtx, cancel := context.WithTimeout(ctx, simulatorProbeTimeout)
		defer cancel()
		result, err := d.proc.Run(probeCtx, "xcrun", "simctl", "spawn", d.opts.UDID, "launchctl", "list")
		if err == nil {
			sessions = parseSimulatorForeignAutomation(result.Stdout, own)
		}
	} else {
		probeCtx, cancel := context.WithTimeout(ctx, deviceProbeTimeout)
		defer cancel()
		result, err := d.proc.Run(probeCtx, "xcrun", "devicectl", "device", "info", "processes", "--device", d.opts.UDID, "--json-output", "-")
		if err == nil {
			sessions = parseDeviceForeignAutomation(result.Stdout, own)
		}
	}
	d.foreignMu.Lock()
	d.foreign = sessions
	d.foreignMu.Unlock()
}

// parseSimulatorForeignAutomation extracts foreign automation sessions
// from "launchctl list" output, whose rows are tab separated PID,
// status and label. Only rows with a numeric PID count, because a
// job that exited keeps its row with "-" in the PID column.
func parseSimulatorForeignAutomation(output string, own ownRunnerIdentity) []driver.ForeignAutomationSession {
	seen := map[string]driver.ForeignAutomationSession{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || !isNumeric(fields[0]) {
			continue
		}
		label := fields[2]
		kind, ok := classifyAutomation(label, ".xctrunner")
		if !ok {
			continue
		}
		name := label
		if m := simulatorRunnerLabelPattern.FindStringSubmatch(label); m != nil {
			name = m[1]
		}
		if own.bundleID != "" && name == own.bundleID {
			continue
		}
		seen[name] = driver.ForeignAutomationSession{Name: name, Kind: kind}
	}
	return sortedSessions(seen)
}

type devicectlProcessesResponse struct {
	Result struct {
		RunningProcesses []struct {
			Executable string `json:"executable"`
		} `json:"runningProcesses"`
	} `json:"result"`
}

// parseDeviceForeignAutomation extracts foreign automation sessions
// from "devicectl device info processes" JSON. devicectl reports
// executable paths, not bundle ids, so a runner is recognized by its
// "*-Runner.app" bundle directory or a WebDriverAgent name.
func parseDeviceForeignAutomation(output string, own ownRunnerIdentity) []driver.ForeignAutomationSession {
	var parsed devicectlProcessesResponse
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		return nil
	}
	seen := map[string]driver.ForeignAutomationSession{}
	for _, p := range parsed.Result.RunningProcesses {
		name, ok := deviceRunnerName(p.Executable)
		if !ok {
			continue
		}
		kind, ok := classifyAutomation(name, "-Runner")
		if !ok {
			continue
		}
		if own.appName != "" && name == own.appName {
			continue
		}
		seen[name] = driver.ForeignAutomationSession{Name: name, Kind: kind}
	}
	return sortedSessions(seen)
}

// deviceRunnerName returns the runner app name for an executable URL
// or path that lives inside a "*-Runner.app" bundle, or the executable
// name when it is a WebDriverAgent binary outside such a bundle.
func deviceRunnerName(executable string) (string, bool) {
	if executable == "" {
		return "", false
	}
	p := executable
	if u, err := url.Parse(executable); err == nil && u.Scheme == "file" {
		p = u.Path
	}
	for _, segment := range strings.Split(p, "/") {
		if strings.HasSuffix(segment, "-Runner.app") {
			return strings.TrimSuffix(segment, ".app"), true
		}
	}
	if base := path.Base(p); strings.Contains(strings.ToLower(base), "webdriveragent") {
		return base, true
	}
	return "", false
}

// classifyAutomation maps an identifier to an automation kind. A
// WebDriverAgent name wins over the generic runner suffix, because a
// WebDriverAgent runner carries both.
func classifyAutomation(identifier, runnerSuffix string) (string, bool) {
	if strings.Contains(strings.ToLower(identifier), "webdriveragent") {
		return foreignKindWDA, true
	}
	if strings.Contains(identifier, runnerSuffix) {
		return foreignKindXCUITest, true
	}
	return "", false
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func sortedSessions(seen map[string]driver.ForeignAutomationSession) []driver.ForeignAutomationSession {
	if len(seen) == 0 {
		return nil
	}
	out := make([]driver.ForeignAutomationSession, 0, len(seen))
	for _, s := range seen {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
