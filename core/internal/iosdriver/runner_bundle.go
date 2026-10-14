package iosdriver

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"

	"dev.atomyx/internal/process"
)

const (
	// RunnerAppName is the product name of the Atomyx XCUITest runner
	// app, without the .app suffix.
	RunnerAppName = "AtomyxDriverAgent-Runner"

	// DefaultRunnerBundleID is the bundle id of the runner app built
	// with the default host bundle id. A runner built with a custom
	// bundle id carries a different one, which ResolveRunnerBundleID
	// reads from the built product.
	DefaultRunnerBundleID = "dev.atomyx.driver.host.xctrunner"

	runnerBundleIDSuffix = ".xctrunner"
	runnerInfoPlistName  = "Info.plist"
	bundleIDPlistKey     = "CFBundleIdentifier"
)

// ResolveRunnerBundleID returns the bundle id of the runner app at
// productPath, read from its Info.plist. It returns fallback when the
// path is empty or the plist cannot be read or names no bundle id, so a
// caller always gets an id to act on.
func ResolveRunnerBundleID(ctx context.Context, runner process.Runner, productPath, fallback string) string {
	if productPath == "" || runner == nil {
		return fallback
	}
	plist := filepath.Join(productPath, runnerInfoPlistName)
	result, err := runner.Run(ctx, "plutil", "-convert", "json", "-o", "-", plist)
	if err != nil {
		return fallback
	}
	if id, ok := parseRunnerBundleID([]byte(result.Stdout)); ok {
		return id
	}
	return fallback
}

// parseRunnerBundleID extracts the runner bundle id from the JSON form
// of an Info.plist. The runner app of a UI test bundle always ends its
// id with ".xctrunner", so an id without that suffix gets it appended,
// which keeps the result comparable with the launchd label of the
// running runner.
func parseRunnerBundleID(data []byte) (string, bool) {
	var plist map[string]any
	if err := json.Unmarshal(data, &plist); err != nil {
		return "", false
	}
	id, _ := plist[bundleIDPlistKey].(string)
	id = strings.TrimSpace(id)
	if id == "" {
		return "", false
	}
	if !strings.HasSuffix(id, runnerBundleIDSuffix) {
		id += runnerBundleIDSuffix
	}
	return id, true
}
