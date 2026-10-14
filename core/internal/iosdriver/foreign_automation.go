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

// ForeignAutomation returns the foreign UI automation sessions found by
// the probe that the last Connect started. It never contacts the device
// and never waits for the probe, so it returns nil until the probe has
// finished.
func (d *IosDriver) ForeignAutomation() []driver.ForeignAutomationSession {
	d.foreignMu.Lock()
	defer d.foreignMu.Unlock()
	if len(d.foreign) == 0 {
		return nil
	}
	return append([]driver.ForeignAutomationSession(nil), d.foreign...)
}

var _ driver.ForeignAutomationReporter = (*IosDriver)(nil)

// startForeignAutomationProbe begins a background probe for the target
// and returns immediately, so Connect never waits on a slow device
// listing. The probe outlives the
// ctx passed to Connect and ends only when it finishes, is superseded,
// or Disconnect cancels it. It first stops any probe still running from an earlier
// Connect and clears the cache, which means a reconnect replaces what a
// previous connection found.
func (d *IosDriver) startForeignAutomationProbe(ctx context.Context) {
	probeCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	d.foreignMu.Lock()
	if d.foreignCancel != nil {
		d.foreignCancel()
	}
	d.foreignCancel = cancel
	d.foreign = nil
	d.foreignGeneration++
	generation := d.foreignGeneration
	d.foreignWG.Add(1)
	d.foreignMu.Unlock()

	go func() {
		defer d.foreignWG.Done()
		sessions := d.probeForeignAutomation(probeCtx)
		d.foreignMu.Lock()
		defer d.foreignMu.Unlock()
		// A newer Connect or a Disconnect has superseded this probe, so
		// its result describes a connection that no longer exists.
		if generation == d.foreignGeneration && probeCtx.Err() == nil {
			d.foreign = sessions
		}
	}()
}

// stopForeignAutomationProbe cancels a running probe and discards the
// cached sessions, because they describe a connection that has ended.
func (d *IosDriver) stopForeignAutomationProbe() {
	d.foreignMu.Lock()
	defer d.foreignMu.Unlock()
	if d.foreignCancel != nil {
		d.foreignCancel()
		d.foreignCancel = nil
	}
	d.foreignGeneration++
	d.foreign = nil
}

// probeForeignAutomation lists the automation sessions on the target.
// The probe is best effort: a command or parse failure yields nothing
// and is not reported.
func (d *IosDriver) probeForeignAutomation(ctx context.Context) []driver.ForeignAutomationSession {
	own := ownRunnerIdentity{bundleID: d.opts.OwnRunnerBundleID, appName: d.opts.OwnRunnerAppName}
	if d.opts.Kind == KindSimulator {
		probeCtx, cancel := context.WithTimeout(ctx, simulatorProbeTimeout)
		defer cancel()
		result, err := d.proc.Run(probeCtx, "xcrun", "simctl", "spawn", d.opts.UDID, "launchctl", "list")
		if err != nil {
			return nil
		}
		return parseSimulatorForeignAutomation(result.Stdout, own)
	}
	probeCtx, cancel := context.WithTimeout(ctx, deviceProbeTimeout)
	defer cancel()
	result, err := d.proc.Run(probeCtx, "xcrun", "devicectl", "device", "info", "processes", "--device", d.opts.UDID, "--json-output", "-")
	if err != nil {
		return nil
	}
	return parseDeviceForeignAutomation(result.Stdout, own)
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
