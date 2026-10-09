package config

import (
	"sort"
)

// Field-level validators for the top-level entities in an Atomyx
// project config document: apps, environments, launch policy, report,
// and suites.

// checkEnvironmentAppReferences is the Go equivalent of
// AtomyxConfigSchema's top-level superRefine: every environment's
// `app` must name a declared app id.
func checkEnvironmentAppReferences(apps []AppEntry, environments map[string]EnvironmentEntry, path []any, c *issueCollector) {
	appIDs := make(map[string]bool, len(apps))
	for _, app := range apps {
		appIDs[app.ID] = true
	}
	names := make([]string, 0, len(environments))
	for name := range environments {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		environment := environments[name]
		if !appIDs[environment.App] {
			c.add(withSegment(withSegment(withSegment(path, "environments"), name), "app"),
				"environments.%s.app references unknown app id %q", name, environment.App)
		}
	}
}

func validateApps(root map[string]any, path []any, c *issueCollector) *[]AppEntry {
	fieldPath := withSegment(path, "apps")
	raw, present := root["apps"]
	if !present {
		c.add(fieldPath, "required")
		return nil
	}
	list, isArray := raw.([]any)
	if !isArray {
		c.add(fieldPath, "expected an array")
		return nil
	}
	if len(list) < 1 {
		c.add(fieldPath, "apps requires at least one entry")
		return nil
	}

	failed := false
	entries := make([]AppEntry, len(list))
	for i, item := range list {
		entry, ok := validateAppEntry(item, withSegment(fieldPath, i), c)
		if !ok {
			failed = true
			continue
		}
		entries[i] = *entry
	}
	if failed {
		return nil
	}
	return &entries
}

var appEntryKeys = map[string]bool{"id": true, "platform": true, "appId": true, "artifact": true}

func validateAppEntry(raw any, path []any, c *issueCollector) (*AppEntry, bool) {
	obj, ok := asObject(raw, path, c)
	if !ok {
		return nil, false
	}
	checkUnknownKeys(obj, appEntryKeys, path, c)

	id, idOK := requiredString(obj, "id", 1, path, c)
	platform := validateEnumRequired(obj, "platform", []string{PlatformAndroid, PlatformIOS}, path, c)
	appID, appIDOK := requiredString(obj, "appId", 1, path, c)
	artifact, artifactOK := optionalString(obj, "artifact", 0, path, c)

	if !idOK || platform == nil || !appIDOK || !artifactOK {
		return nil, false
	}
	return &AppEntry{ID: id, Platform: *platform, AppID: appID, Artifact: artifact}, true
}

func validateEnvironments(root map[string]any, path []any, c *issueCollector) *map[string]EnvironmentEntry {
	fieldPath := withSegment(path, "environments")
	raw, present := root["environments"]
	if !present {
		c.add(fieldPath, "required")
		return nil
	}
	obj, isObject := raw.(map[string]any)
	if !isObject {
		c.add(fieldPath, "expected an object")
		return nil
	}

	failed := false
	entries := make(map[string]EnvironmentEntry, len(obj))
	names := make([]string, 0, len(obj))
	for name := range obj {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		entry, ok := validateEnvironmentEntry(obj[name], withSegment(fieldPath, name), c)
		if !ok {
			failed = true
			continue
		}
		entries[name] = *entry
	}
	if failed {
		return nil
	}
	return &entries
}

var environmentEntryKeys = map[string]bool{"app": true, "vars": true, "device": true, "proxy": true}

func validateEnvironmentEntry(raw any, path []any, c *issueCollector) (*EnvironmentEntry, bool) {
	obj, ok := asObject(raw, path, c)
	if !ok {
		return nil, false
	}
	checkUnknownKeys(obj, environmentEntryKeys, path, c)

	app, appOK := requiredString(obj, "app", 1, path, c)
	vars, varsOK := optionalStringMap(obj, "vars", path, c)
	device, deviceOK := optionalString(obj, "device", 1, path, c)
	proxy, proxyOK := optionalString(obj, "proxy", 0, path, c)

	if !appOK || !varsOK || !deviceOK || !proxyOK {
		return nil, false
	}
	return &EnvironmentEntry{App: app, Vars: vars, Device: device, Proxy: proxy}, true
}

var launchPolicyKeys = map[string]bool{"mode": true, "clearData": true}

func validateLaunchPolicy(root map[string]any, path []any, c *issueCollector) *LaunchPolicy {
	raw, present := root["launch"]
	if !present || raw == nil {
		return nil
	}
	fieldPath := withSegment(path, "launch")
	obj, ok := asObject(raw, fieldPath, c)
	if !ok {
		return nil
	}
	checkUnknownKeys(obj, launchPolicyKeys, fieldPath, c)

	mode := validateEnum(obj, "mode", []string{LaunchModeFresh, LaunchModeAttach}, fieldPath, c)
	clearData, clearDataOK := optionalBool(obj, "clearData", fieldPath, c)
	if !clearDataOK {
		return nil
	}
	return &LaunchPolicy{Mode: mode, ClearData: clearData}
}

var reportKeys = map[string]bool{"formats": true, "outDir": true}

func validateReport(root map[string]any, path []any, c *issueCollector) *Report {
	raw, present := root["report"]
	if !present || raw == nil {
		return nil
	}
	fieldPath := withSegment(path, "report")
	obj, ok := asObject(raw, fieldPath, c)
	if !ok {
		return nil
	}
	checkUnknownKeys(obj, reportKeys, fieldPath, c)

	formats := validateReportFormats(obj, fieldPath, c)
	outDir, outDirOK := requiredString(obj, "outDir", 1, fieldPath, c)
	if formats == nil || !outDirOK {
		return nil
	}
	return &Report{Formats: formats, OutDir: outDir}
}

func validateReportFormats(obj map[string]any, path []any, c *issueCollector) []string {
	fieldPath := withSegment(path, "formats")
	raw, present := obj["formats"]
	if !present {
		c.add(fieldPath, "required")
		return nil
	}
	list, isArray := raw.([]any)
	if !isArray {
		c.add(fieldPath, "expected an array")
		return nil
	}
	if len(list) < 1 {
		c.add(fieldPath, "report.formats requires at least one format")
		return nil
	}
	failed := false
	formats := make([]string, len(list))
	for i, item := range list {
		s, isStr := item.(string)
		if !isStr || (s != ReportFormatHTML && s != ReportFormatJSON) {
			c.add(withSegment(fieldPath, i), "expected 'html' or 'json'")
			failed = true
			continue
		}
		formats[i] = s
	}
	if failed {
		return nil
	}
	return formats
}

var sessionKeys = map[string]bool{
	"mcpIdleTimeoutMs": true, "openRunIdleTimeoutMs": true, "deviceAcquireTimeoutMs": true,
	"debugPauseTimeoutMs": true,
}

func validateSession(root map[string]any, path []any, c *issueCollector) *Session {
	raw, present := root["session"]
	if !present || raw == nil {
		return nil
	}
	fieldPath := withSegment(path, "session")
	obj, ok := asObject(raw, fieldPath, c)
	if !ok {
		return nil
	}
	checkUnknownKeys(obj, sessionKeys, fieldPath, c)
	mcpIdle, _ := optionalPositiveInt(obj, "mcpIdleTimeoutMs", fieldPath, c)
	openRun, _ := optionalPositiveInt(obj, "openRunIdleTimeoutMs", fieldPath, c)
	acquire, _ := optionalPositiveInt(obj, "deviceAcquireTimeoutMs", fieldPath, c)
	debugPause, _ := optionalPositiveInt(obj, "debugPauseTimeoutMs", fieldPath, c)
	return &Session{McpIdleTimeoutMs: mcpIdle, OpenRunIdleTimeoutMs: openRun, DeviceAcquireTimeoutMs: acquire, DebugPauseTimeoutMs: debugPause}
}

func validateSuites(root map[string]any, path []any, c *issueCollector) *map[string]SuiteEntry {
	raw, present := root["suites"]
	if !present || raw == nil {
		return nil
	}
	fieldPath := withSegment(path, "suites")
	obj, isObject := raw.(map[string]any)
	if !isObject {
		c.add(fieldPath, "expected an object")
		return nil
	}

	failed := false
	entries := make(map[string]SuiteEntry, len(obj))
	names := make([]string, 0, len(obj))
	for name := range obj {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		entry, ok := validateSuiteEntry(obj[name], withSegment(fieldPath, name), c)
		if !ok {
			failed = true
			continue
		}
		entries[name] = *entry
	}
	if failed {
		return nil
	}
	return &entries
}

var suiteEntryKeys = map[string]bool{"tags": true, "globs": true, "retries": true, "quarantine": true}

func validateSuiteEntry(raw any, path []any, c *issueCollector) (*SuiteEntry, bool) {
	obj, ok := asObject(raw, path, c)
	if !ok {
		return nil, false
	}
	checkUnknownKeys(obj, suiteEntryKeys, path, c)

	tagsLen, tagsAborted := rawArrayPresence(obj, "tags")
	globsLen, globsAborted := rawArrayPresence(obj, "globs")

	tags := optionalStringArray(obj, "tags", 1, path, c)
	globs := optionalStringArray(obj, "globs", 1, path, c)
	quarantine := optionalStringArray(obj, "quarantine", 1, path, c)
	retries, retriesOk := optionalIntInRange(obj, "retries", 0, MaxSuiteRetries, path, c)

	// zod's nested `.refine()` on SuiteEntrySchema reads tags/globs
	// from the object schema's own parse result, not from a second
	// pass over already-validated data. An item that fails its own
	// length check (`z.string().min(1)`) marks that array "dirty" but
	// not "aborted", so the array keeps its original length and the
	// refine still runs against it. Only a field of the wrong type
	// (not an array at all) aborts the object schema outright, which
	// skips the refine entirely - so a lone "expected an array" issue
	// must not be joined by this one.
	if tagsAborted || globsAborted || !retriesOk {
		return nil, false
	}
	if tagsLen == 0 && globsLen == 0 {
		c.add(path, "a suite requires at least one of `tags` or `globs`")
		return nil, false
	}
	return &SuiteEntry{Tags: tags, Globs: globs, Retries: retries, Quarantine: quarantine}, true
}

// rawArrayPresence reports the length of obj[key] as originally
// supplied, before any item-level validation, along with whether the
// field's own type check failed (present but not an array). A field
// that is absent, or an array whose items are individually invalid,
// is not "aborted" - only a non-array value is.
func rawArrayPresence(obj map[string]any, key string) (length int, aborted bool) {
	raw, present := obj[key]
	if !present || raw == nil {
		return 0, false
	}
	list, isArray := raw.([]any)
	if !isArray {
		return 0, true
	}
	return len(list), false
}
