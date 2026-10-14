package config

const (
	// PlatformAndroid and PlatformIOS are the two values AppEntry.Platform accepts.
	PlatformAndroid = "android"
	PlatformIOS     = "ios"

	// LaunchModeFresh and LaunchModeAttach are the two values LaunchPolicy.Mode accepts.
	LaunchModeFresh  = "fresh"
	LaunchModeAttach = "attach"

	// PreconditionsSkip and PreconditionsForce are the two values AtomyxConfig.Preconditions accepts.
	PreconditionsSkip  = "skip"
	PreconditionsForce = "force"

	// VideoOff and VideoOnFailure are the two values AtomyxConfig.Video accepts.
	// VideoOnFailure records the device screen and keeps the video of a run
	// that failed.
	VideoOff       = "off"
	VideoOnFailure = "onFailure"

	// ReportFormatHTML, ReportFormatJSON and ReportFormatJUnit are the values
	// a Report.Formats entry accepts.
	ReportFormatHTML  = "html"
	ReportFormatJSON  = "json"
	ReportFormatJUnit = "junit"

	// MaxSuiteRetries is the most retries a suite entry may declare.
	MaxSuiteRetries = 3
)

// AppEntry is one app variant a project can run, matching
// AppEntrySchema in atomyx-config.schema.ts.
type AppEntry struct {
	ID       string  `json:"id"`
	Platform string  `json:"platform"`
	AppID    string  `json:"appId"`
	Artifact *string `json:"artifact,omitempty"`
}

// EnvironmentEntry is one named environment, matching
// EnvironmentEntrySchema.
//
// Vars is a pointer so a document that writes `vars: {}` marshals as
// a present, empty object, distinct from omitting `vars` entirely, because
// encoding/json's omitempty treats a nil and a zero-length map the
// same way, which a plain (non-pointer) map field cannot avoid.
type EnvironmentEntry struct {
	App    string             `json:"app"`
	Vars   *map[string]string `json:"vars,omitempty"`
	Device *string            `json:"device,omitempty"`
	Proxy  *string            `json:"proxy,omitempty"`
	// EnvRefVars names the Vars entries whose value came from a
	// `${ENV:NAME}` reference. It is empty until ResolveEnvRefs runs.
	EnvRefVars map[string]bool `json:"-"`
	// EnvRefValues holds the process environment values the `${ENV:NAME}`
	// references of Vars resolved to. A var that mixes a literal with a
	// reference holds the whole string, so the value alone is listed here
	// for a host that masks secrets by value.
	EnvRefValues []string `json:"-"`
}

// LaunchPolicy is the project-wide default launch policy, matching
// LaunchPolicySchema.
type LaunchPolicy struct {
	Mode      *string `json:"mode,omitempty"`
	ClearData *bool   `json:"clearData,omitempty"`
}

// Report is the default report output configuration, matching
// ReportSchema.
type Report struct {
	Formats []string `json:"formats"`
	OutDir  string   `json:"outDir"`
}

// Retention is the project's run retention policy. KeepLast retains the
// newest KeepLast top-level runs and OlderThanDays selects runs that
// started more than that many days ago. At least one field is set, and
// when both are set a run must satisfy both to be deleted.
type Retention struct {
	KeepLast      *int64 `json:"keepLast,omitempty"`
	OlderThanDays *int64 `json:"olderThanDays,omitempty"`
}

// Session tunes the device session timeouts shared by the CLI and the
// MCP server. Every field is optional, and an absent field keeps its
// default. Values are positive millisecond counts.
type Session struct {
	McpIdleTimeoutMs       *int64 `json:"mcpIdleTimeoutMs,omitempty"`
	OpenRunIdleTimeoutMs   *int64 `json:"openRunIdleTimeoutMs,omitempty"`
	DeviceAcquireTimeoutMs *int64 `json:"deviceAcquireTimeoutMs,omitempty"`
	DebugPauseTimeoutMs    *int64 `json:"debugPauseTimeoutMs,omitempty"`
}

// SuiteEntry is one named test selector, matching SuiteEntrySchema.
//
// Tags and Globs are pointers for the same reason as
// EnvironmentEntry.Vars: a present-but-empty array must marshal as
// `[]`, not be dropped the way omitempty would drop a plain
// zero-length slice.
type SuiteEntry struct {
	Tags  *[]string `json:"tags,omitempty"`
	Globs *[]string `json:"globs,omitempty"`
	// Retries is how many more times a failed entry of the suite runs
	// after its first attempt, from 0 to MaxSuiteRetries. Absent means 0.
	Retries *int64 `json:"retries,omitempty"`
	// Quarantine lists globs, matched like Globs against an entry path,
	// of entries whose failure does not fail the suite.
	Quarantine *[]string `json:"quarantine,omitempty"`
}

// AtomyxConfig is the top-level Atomyx project config, matching
// AtomyxConfigSchema and its z.infer'd AtomyxConfig type.
//
// EnvironmentOrder holds the declaration order of Environments' keys
// as they appeared in the source YAML document, carried alongside the
// map only so SelectEnvironment can list "Available environments" in
// a stable, reproducible order, since a plain Go map has no iteration
// order of its own.
type AtomyxConfig struct {
	Project          string                      `json:"project"`
	Apps             []AppEntry                  `json:"apps"`
	Environments     map[string]EnvironmentEntry `json:"environments"`
	EnvironmentOrder []string                    `json:"-"`
	Launch           *LaunchPolicy               `json:"launch,omitempty"`
	Preconditions    *string                     `json:"preconditions,omitempty"`
	Report           *Report                     `json:"report,omitempty"`
	Video            *string                     `json:"video,omitempty"`
	Retention        *Retention                  `json:"retention,omitempty"`
	Session          *Session                    `json:"session,omitempty"`
	Suites           *map[string]SuiteEntry      `json:"suites,omitempty"`
	TestRoots        *[]string                   `json:"testRoots,omitempty"`
	DataRoots        *[]string                   `json:"dataRoots,omitempty"`
}

// LoadedConfig is a project config that was found and validated, plus
// the absolute path it was read from. Every `${ENV:NAME}` placeholder
// inside an environment's `vars` has already been substituted from
// the process environment by the time a LoadedConfig exists.
type LoadedConfig struct {
	Path   string
	Config AtomyxConfig
}

// LoadOptions selects how Load locates a config file. An empty
// ConfigPath means discovery; a non-empty one bypasses discovery and
// is resolved relative to Cwd.
type LoadOptions struct {
	Cwd        string
	ConfigPath string
}

// ResolvedEnvironment is one config environment resolved to concrete
// values a run can use directly: the app id it targets, its
// variables, and the device and proxy defaults it declares.
//
// Platform always comes from the selected app's own Platform field,
// never from the environment. An environment has no platform
// override.
type ResolvedEnvironment struct {
	Name  string            `json:"name"`
	AppID string            `json:"appId"`
	Vars  map[string]string `json:"vars"`
	// EnvRefVars names the Vars entries whose value came from a
	// `${ENV:NAME}` reference. Every other entry is a literal written in
	// the config file.
	EnvRefVars map[string]bool `json:"-"`
	// EnvRefValues holds the process environment values the references
	// of Vars resolved to, apart from the strings they sit in.
	EnvRefValues []string `json:"-"`
	Platform     string   `json:"platform"`
	DeviceID     *string  `json:"deviceId,omitempty"`
	Proxy        *string  `json:"proxy,omitempty"`
	Artifact     *string  `json:"artifact,omitempty"`
}

// VideoOnFailure reports whether the config asks for the screen
// recording of a failed run to be kept. It is false for a nil config and
// for a config that leaves video unset, which is the default.
func (c *AtomyxConfig) VideoOnFailure() bool {
	return c != nil && c.Video != nil && *c.Video == VideoOnFailure
}
