package conformance

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"testing"

	"dev.atomyx/internal/config"
)

// configFixturePath is the path fed into every case: the loader's
// user-facing messages embed the config file's path, so this literal
// string must stay the one the frozen golden was recorded with, for
// that text to compare byte for byte.
const configFixturePath = "/atomyx.config.yaml"

// ConfigOracleResponse is the shape one config-loading outcome is
// normalized into for a structural diff against the frozen golden at
// goldens/config/<name>.json: whether a config file was found, the
// schema-validity flag and any resolved config or selected
// environment, and the exact wording of an env-ref or environment-
// selection error.
type ConfigOracleResponse struct {
	Found       bool            `json:"found"`
	SchemaValid *bool           `json:"schemaValid"`
	IssuePaths  [][]any         `json:"issuePaths"`
	EnvRefError *string         `json:"envRefError"`
	Config      json.RawMessage `json:"config"`
	SelectError *string         `json:"selectError"`
	Resolved    json.RawMessage `json:"resolved"`
}

// goConfigResponse replays a ConfigCase against the Go config package
// and normalizes its outcome into a ConfigOracleResponse for a
// structural diff against the frozen golden.
func goConfigResponse(t *testing.T, c *ConfigCase) *ConfigOracleResponse {
	t.Helper()

	if c.NoConfig {
		hasName := c.EnvName != nil
		name := ""
		if hasName {
			name = *c.EnvName
		}
		resolved, err := config.SelectEnvironment(nil, name, hasName)
		return &ConfigOracleResponse{
			Found:       false,
			SelectError: errString(err),
			Resolved:    marshalOrNil(t, resolved),
		}
	}

	cfg, issues := config.ParseAndValidate([]byte(c.Yaml))
	if len(issues) > 0 {
		valid := false
		return &ConfigOracleResponse{
			Found:       true,
			SchemaValid: &valid,
			IssuePaths:  issuePaths(issues),
		}
	}

	for key, value := range c.EnvVars {
		t.Setenv(key, value)
	}
	resolvedConfig, err := config.ResolveEnvRefs(*cfg, configFixturePath)
	if err != nil {
		valid := true
		return &ConfigOracleResponse{
			Found:       true,
			SchemaValid: &valid,
			EnvRefError: errString(err),
		}
	}

	hasName := c.EnvName != nil
	name := ""
	if hasName {
		name = *c.EnvName
	}
	loaded := &config.LoadedConfig{Path: configFixturePath, Config: resolvedConfig}
	resolvedEnv, selectErr := config.SelectEnvironment(loaded, name, hasName)

	valid := true
	return &ConfigOracleResponse{
		Found:       true,
		SchemaValid: &valid,
		Config:      marshalOrNil(t, resolvedConfig),
		SelectError: errString(selectErr),
		Resolved:    marshalOrNil(t, resolvedEnv),
	}
}

func errString(err error) *string {
	if err == nil {
		return nil
	}
	s := err.Error()
	return &s
}

func marshalOrNil(t *testing.T, v any) json.RawMessage {
	t.Helper()
	if v == nil {
		return nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshaling %#v: %v", v, err)
	}
	return raw
}

// issuePaths extracts every issue's path, unsorted: toComparable sorts
// the result before it is diffed against the golden, so the order Go
// happened to produce its issues in never matters.
func issuePaths(issues []config.ValidationIssue) [][]any {
	paths := make([][]any, len(issues))
	for i, issue := range issues {
		paths[i] = issue.Path
	}
	return paths
}

// configComparable is the subset of ConfigOracleResponse the
// differential test diffs. issuePaths is normalized (sorted) on both
// sides before comparison, because neither zod's internal validation
// order nor this package's map-keyed validation order is guaranteed
// to match the other's, and only the resulting set of violated paths
// is part of the schema's contract.
type configComparable struct {
	Found       bool            `json:"found"`
	SchemaValid *bool           `json:"schemaValid"`
	IssuePaths  [][]any         `json:"issuePaths"`
	EnvRefError *string         `json:"envRefError"`
	Config      json.RawMessage `json:"config"`
	SelectError *string         `json:"selectError"`
	Resolved    json.RawMessage `json:"resolved"`
}

func toComparable(r *ConfigOracleResponse) configComparable {
	paths := r.IssuePaths
	if paths == nil {
		paths = [][]any{}
	}
	sort.Slice(paths, func(i, j int) bool {
		bi, _ := json.Marshal(paths[i])
		bj, _ := json.Marshal(paths[j])
		return string(bi) < string(bj)
	})
	return configComparable{
		Found:       r.Found,
		SchemaValid: r.SchemaValid,
		IssuePaths:  paths,
		EnvRefError: r.EnvRefError,
		Config:      r.Config,
		SelectError: r.SelectError,
		Resolved:    r.Resolved,
	}
}

// TestConfigDifferential diffs the Go config package's output over
// every corpus/config case against a frozen golden fixture, covering
// valid configs, environment-profile selection, and validation-error
// cases. Schema-validation issue text is never compared. See
// configComparable's doc comment, but every hand-authored message
// (an unresolved ${ENV:NAME} reference, an unknown or ambiguous
// --env selection) is compared byte for byte via EnvRefError and
// SelectError.
func TestConfigDifferential(t *testing.T) {
	cases, err := LoadConfigCorpus("corpus/config")
	if err != nil {
		t.Fatalf("loading config corpus: %v", err)
	}

	for _, c := range cases {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			goResult := goConfigResponse(t, c)
			goComparable, err := json.Marshal(toComparable(goResult))
			if err != nil {
				t.Fatalf("marshaling Go comparable: %v", err)
			}

			checkGolden(t, filepath.Join("config", c.Name+".json"), goComparable, DiffJSON)
		})
	}
}
