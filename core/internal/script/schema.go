package script

// PreconditionCheck is the condition a precondition checks before its
// flow runs. Exactly one of Visible / NotVisible must be set, since
// ParseScript rejects a script where both or neither are set.
type PreconditionCheck struct {
	Visible    *StringOrSelector `json:"visible,omitempty"`
	NotVisible *StringOrSelector `json:"notVisible,omitempty"`
}

// Precondition is a run-once setup gate a flow's steps require before
// they run: Check is evaluated first, and Setup runs when it is
// unmet, according to Policy ("ensure", "always", or "skip";
// defaults to "ensure" when nil).
type Precondition struct {
	Check  PreconditionCheck `json:"check"`
	Setup  StepsOrFile       `json:"setup"`
	Policy *string           `json:"policy,omitempty"`
}

// ScriptDefaults are the fallbacks ParseScript applies when a field
// is absent from the script's config document.
var ScriptDefaults = struct {
	Format string
	Name   string
}{
	Format: "atomyx/v1",
	Name:   "Untitled script",
}

// ScriptDefinition is a parsed and validated Atomyx YAML test script,
// in either of its two physical forms: an entry script (full config
// plus steps, separated by a `---` document boundary) or a flow
// fragment (steps only, executing in a parent script's context).
type ScriptDefinition struct {
	Format *string `json:"format,omitempty"`
	AppID  string  `json:"appId"`
	// AppIDSource is AppID as written, before `${var}` interpolation. It
	// is empty for a definition built without parsing a script.
	AppIDSource   string            `json:"-"`
	Name          string            `json:"name"`
	Description   *string           `json:"description,omitempty"`
	Precondition  *string           `json:"precondition,omitempty"`
	Tags          []string          `json:"tags,omitempty"`
	Env           map[string]string `json:"env"`
	Proxy         *string           `json:"proxy,omitempty"`
	Requires      []string          `json:"requires,omitempty"`
	Preconditions []Precondition    `json:"preconditions,omitempty"`
	StepDelay     *float64          `json:"stepDelay,omitempty"`
	Steps         []ScriptStep      `json:"steps"`
	StepLines     []int             `json:"_stepLines,omitempty"`
}

// AuthoredAppID returns the app id as the script author wrote it, so
// text that names the app does not carry a value a variable supplied.
func (d *ScriptDefinition) AuthoredAppID() string {
	if d.AppIDSource != "" {
		return d.AppIDSource
	}
	return d.AppID
}
