package iosdriver

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/process"
)

// appNotInstalledPatterns are substrings simctl and devicectl are
// known to include when a command fails because the given bundle id
// has no matching installed app, as opposed to a transport, daemon,
// or device failure. They decide whether an app-lifecycle command
// failure means "confirmed not installed" or a genuine error the
// caller must see.
var appNotInstalledPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)failed to find`),
	regexp.MustCompile(`(?i)no such file or directory`),
	regexp.MustCompile(`(?i)does not contain`),
	regexp.MustCompile(`(?i)is not installed`),
	regexp.MustCompile(`(?i)no matching application`),
}

// isAppNotInstalledError reports whether err (with the command's
// captured output in result) looks like a simctl or devicectl failure
// caused by the bundle id not being installed, based on the message
// text those tools are known to emit for that case.
func isAppNotInstalledError(err error, result process.Result) bool {
	text := result.Stdout + " " + result.Stderr
	if err != nil {
		text = err.Error() + " " + text
	}
	for _, p := range appNotInstalledPatterns {
		if p.MatchString(text) {
			return true
		}
	}
	return false
}

// ListApps lists installed apps host-side: the Swift runner exposes
// no listApps command, so this shells out to xcrun directly.
func (d *IosDriver) ListApps(ctx context.Context) ([]driver.InstalledApp, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if d.opts.Kind == KindSimulator {
		return d.listAppsSimulator(ctx), nil
	}
	return d.listAppsDevice(ctx)
}

type simctlAppInfo struct {
	CFBundleDisplayName string `json:"CFBundleDisplayName"`
	CFBundleName        string `json:"CFBundleName"`
}

// listAppsSimulator pipes "simctl listapps" plist output through
// plutil to get JSON. A shell is required because the pipeline
// composes two commands. A failure of any kind (missing shell tools,
// unparsable output) reports zero apps rather than an error, so a
// simulator missing its shell tools is treated as having no
// installed apps instead of failing the caller.
func (d *IosDriver) listAppsSimulator(ctx context.Context) []driver.InstalledApp {
	script := fmt.Sprintf("xcrun simctl listapps '%s' | plutil -convert json -o - -- -", d.opts.UDID)
	result, err := d.proc.Run(ctx, "sh", "-c", script)
	if err != nil {
		return []driver.InstalledApp{}
	}
	var parsed map[string]simctlAppInfo
	if err := json.Unmarshal([]byte(result.Stdout), &parsed); err != nil {
		return []driver.InstalledApp{}
	}
	out := make([]driver.InstalledApp, 0, len(parsed))
	for bundleID, info := range parsed {
		displayName := info.CFBundleDisplayName
		if displayName == "" {
			displayName = info.CFBundleName
		}
		if displayName == "" {
			displayName = bundleID
		}
		out = append(out, driver.InstalledApp{BundleID: bundleID, DisplayName: displayName})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].BundleID < out[j].BundleID })
	return out
}

// listAppsDevice propagates a fetchDeviceApps failure instead of
// reporting an empty app list, so a devicectl or connectivity failure
// surfaces to the caller rather than reading as "0 apps installed".
func (d *IosDriver) listAppsDevice(ctx context.Context) ([]driver.InstalledApp, error) {
	return d.fetchDeviceApps(ctx)
}

type devicectlAppsResponse struct {
	Result struct {
		Apps []struct {
			BundleIdentifier string `json:"bundleIdentifier"`
			Name             string `json:"name"`
		} `json:"apps"`
	} `json:"result"`
}

// fetchDeviceApps runs "devicectl device info apps" and parses its
// app list. devicectl lists only developer-installed apps unless told
// to include default apps, so --include-default-apps is required for
// system and App Store apps such as com.apple.Preferences to appear,
// and for installation checks to see them. A command or decode failure propagates rather than
// collapsing into an empty result, so a caller can distinguish
// "confirmed empty result" from "the query itself failed".
func (d *IosDriver) fetchDeviceApps(ctx context.Context) ([]driver.InstalledApp, error) {
	result, err := d.proc.Run(ctx, "xcrun", "devicectl", "device", "info", "apps", "--device", d.opts.UDID, "--include-default-apps", "--json-output", "-")
	if err != nil {
		return nil, err
	}
	var parsed devicectlAppsResponse
	if err := json.Unmarshal([]byte(result.Stdout), &parsed); err != nil {
		return nil, fmt.Errorf("iosdriver: decode devicectl apps: %w", err)
	}
	out := make([]driver.InstalledApp, 0, len(parsed.Result.Apps))
	for _, a := range parsed.Result.Apps {
		if a.BundleIdentifier == "" {
			continue
		}
		displayName := a.Name
		if displayName == "" {
			displayName = a.BundleIdentifier
		}
		out = append(out, driver.InstalledApp{BundleID: a.BundleIdentifier, DisplayName: displayName})
	}
	return out, nil
}

// InstallApp installs from a host-side artifact path. Both the
// simulator and device commands reach xcrun directly, because the
// driver has no other host-exec mechanism available for iOS
// lifecycle management.
func (d *IosDriver) InstallApp(ctx context.Context, source string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d.opts.Kind == KindSimulator {
		_, err := d.proc.Run(ctx, "xcrun", "simctl", "install", d.opts.UDID, source)
		return err
	}
	_, err := d.proc.Run(ctx, "xcrun", "devicectl", "device", "install", "app", "--device", d.opts.UDID, source)
	return err
}

func (d *IosDriver) runUninstall(ctx context.Context, appID string) (process.Result, error) {
	if d.opts.Kind == KindSimulator {
		return d.proc.Run(ctx, "xcrun", "simctl", "uninstall", d.opts.UDID, appID)
	}
	return d.proc.Run(ctx, "xcrun", "devicectl", "device", "uninstall", "app", "--device", d.opts.UDID, appID)
}

func (d *IosDriver) UninstallApp(ctx context.Context, appID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := d.runUninstall(ctx, appID)
	return err
}

// clearAppDataArtifactRemedy is the next step reported when a data
// clear is requested for an app that has no artifact to reinstall.
const clearAppDataArtifactRemedy = `Set the "artifact" field of this app in the project config to the path of the installable app, then run again.`

// ClearAppData resets appID to a clean data state. iOS exposes no
// primitive that clears an app's data in place, so the only path to a
// clean state is uninstall then reinstall from the artifact, and
// opts.Source is required: this returns a diag.CodeInvalidArgument
// error naming the "artifact" config field when it is empty, instead
// of silently no-op-ing. The uninstall step tolerates only a
// confirmed "not installed" failure, because a device that never had
// the app installed is already in the target "no app data" state and
// the following install still needs to run; any other uninstall
// failure, including a canceled context, is returned instead of
// masked.
func (d *IosDriver) ClearAppData(ctx context.Context, appID string, opts driver.ClearAppDataOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if opts.Source == "" {
		return diag.New(diag.CodeInvalidArgument,
			fmt.Sprintf("cannot clear the data of %s because the app has no installable artifact configured", appID),
			clearAppDataArtifactRemedy,
			nil)
	}
	result, err := d.runUninstall(ctx, appID)
	if err != nil {
		if ctx.Err() != nil || !isAppNotInstalledError(err, result) {
			return err
		}
	}
	return d.InstallApp(ctx, opts.Source)
}

func (d *IosDriver) IsAppInstalled(ctx context.Context, appID string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if d.opts.Kind == KindSimulator {
		return d.isAppInstalledSimulator(ctx, appID)
	}
	return d.isAppInstalledDevice(ctx, appID)
}

// isAppInstalledSimulator queries the app's container path directly
// with "simctl get_app_container" instead of delegating to ListApps,
// so a transient simctl failure (CoreSimulatorService down, simulator
// not booted) surfaces as an error instead of a false "not installed"
// result. It reports false only when the failure matches simctl's
// known "no such bundle" phrasing; anything else, including a
// canceled context, is returned as an error.
func (d *IosDriver) isAppInstalledSimulator(ctx context.Context, appID string) (bool, error) {
	result, err := d.proc.Run(ctx, "xcrun", "simctl", "get_app_container", d.opts.UDID, appID, "app")
	if err == nil {
		return true, nil
	}
	if ctx.Err() != nil {
		return false, err
	}
	if isAppNotInstalledError(err, result) {
		return false, nil
	}
	return false, err
}

// isAppInstalledDevice reuses the full app listing query, because
// devicectl has no per-bundle lookup. Unlike ListApps, a query
// failure here propagates instead of being read as "not installed",
// so a disconnected device surfaces as an error rather than a false
// negative.
func (d *IosDriver) isAppInstalledDevice(ctx context.Context, appID string) (bool, error) {
	apps, err := d.fetchDeviceApps(ctx)
	if err != nil {
		return false, err
	}
	for _, a := range apps {
		if a.BundleID == appID {
			return true, nil
		}
	}
	return false, nil
}
