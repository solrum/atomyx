package runfolder

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func fullMeta() Meta {
	completed := int64(1_790_000_005_000)
	failedAt := 3
	summary := "done"
	return Meta{
		SchemaVersion: SchemaVersion,
		RunID:         "20261002-101500-pixel-7-a1b2",
		Kind:          KindScript,
		ScriptPath:    "flows/login.yml",
		ScriptName:    "login",
		DeviceID:      "emulator-5554",
		Device:        &Device{Platform: "android", Kind: "emulator", HardwareID: "hw-1", Name: "Pixel 7"},
		Host:          &Host{AtomyxVersion: "0.1.0", AgentVersion: "0.1.0"},
		StartedAt:     1_790_000_000_000,
		CompletedAt:   &completed,
		Result:        ResultFailed,
		Outcome:       OutcomeError,
		FailedAtStep:  &failedAt,
		TotalSteps:    9,
		MCP: &MCPInfo{
			Name: "smoke", Source: "mcp", ActionCount: 4, DurationMs: 5000, Summary: &summary,
			Findings: []Finding{{ID: "b1", Title: "t", Description: "d", ScreenshotPath: "artifacts/b1.png", Timestamp: 1_790_000_001_000}},
		},
	}
}

func TestMetaRoundTripMatchesGolden(t *testing.T) {
	encoded, err := json.MarshalIndent(fullMeta(), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	encoded = append(encoded, '\n')

	golden := filepath.Join("testdata", "meta-full.json")
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("reading golden: %v", err)
	}
	if !bytes.Equal(encoded, want) {
		t.Fatalf("encoded meta differs from %s:\n%s", golden, encoded)
	}

	var decoded Meta
	if err := json.Unmarshal(want, &decoded); err != nil {
		t.Fatal(err)
	}
	again, err := json.MarshalIndent(decoded, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(append(again, '\n'), want) {
		t.Fatalf("decode then encode changed the document:\n%s", again)
	}
}

func TestMetaDecodeIgnoresUnknownFields(t *testing.T) {
	var m Meta
	doc := `{"schemaVersion":2,"runId":"x","kind":"mcp","startedAt":1,"future":{"a":1}}`
	if err := json.Unmarshal([]byte(doc), &m); err != nil {
		t.Fatalf("unknown field rejected: %v", err)
	}
	if m.RunID != "x" || m.CompletedAt != nil {
		t.Fatalf("unexpected decode: %+v", m)
	}
}

func TestEveryOutcomeMapsToAResult(t *testing.T) {
	for _, o := range Outcomes() {
		if _, ok := ResultOf(o); !ok {
			t.Errorf("outcome %q has no result", o)
		}
	}
	if _, ok := ResultOf("bogus"); ok {
		t.Error("unknown outcome mapped")
	}
}

func TestFromLegacyStatus(t *testing.T) {
	cases := []struct {
		status  string
		result  Result
		outcome Outcome
	}{
		{"passed", ResultPassed, OutcomePassed},
		{"failed", ResultFailed, OutcomeFailed},
		{"error", ResultFailed, OutcomeError},
		{"cancelled", ResultCancelled, OutcomeCancelled},
		{"running", ResultCancelled, OutcomeInterrupted},
	}
	for _, c := range cases {
		r, o, ok := FromLegacyStatus(c.status)
		if !ok || r != c.result || o != c.outcome {
			t.Errorf("%s: got %q %q %v", c.status, r, o, ok)
		}
	}
	if _, _, ok := FromLegacyStatus("paused"); ok {
		t.Error("unknown status mapped")
	}
}

func TestRunDirLayout(t *testing.T) {
	got := RunDir("/r", "id1")
	if got != filepath.Join("/r", "runs", "v1", "id1") {
		t.Fatalf("RunDir = %s", got)
	}
	if ArtifactsDir(got) != filepath.Join(got, "artifacts") || ReportDir(got) != filepath.Join(got, "report") {
		t.Fatal("sub directory names drifted")
	}
	if LooseScreenshotsDir("/r") != filepath.Join("/r", "screenshots") {
		t.Fatal("loose screenshots dir drifted")
	}
}

func TestMCPInfoOmitsNilFindingsAndKeepsEmptySummary(t *testing.T) {
	empty := ""
	withEmpty, err := json.Marshal(MCPInfo{Name: "n", Source: "s", Summary: &empty})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(withEmpty, []byte(`"findings"`)) {
		t.Errorf("nil findings encoded: %s", withEmpty)
	}
	if !bytes.Contains(withEmpty, []byte(`"summary":""`)) {
		t.Errorf("empty summary dropped: %s", withEmpty)
	}
	missing, err := json.Marshal(MCPInfo{Name: "n", Source: "s"})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(missing, []byte(`"summary"`)) {
		t.Errorf("absent summary encoded: %s", missing)
	}
}

// jsonFields lists the json property names of a struct type and which
// of them are required, meaning tagged without omitempty.
func jsonFields(t reflect.Type) (all, required []string) {
	for i := 0; i < t.NumField(); i++ {
		name, opts, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		all = append(all, name)
		if !strings.Contains(opts, "omitempty") {
			required = append(required, name)
		}
	}
	sort.Strings(all)
	sort.Strings(required)
	return all, required
}

func TestSchemaMatchesJSONTags(t *testing.T) {
	defs := GenerateJSONSchema()["definitions"].(map[string]any)
	types := map[string]reflect.Type{
		"RunMeta": reflect.TypeOf(Meta{}),
		"Device":  reflect.TypeOf(Device{}),
		"Host":    reflect.TypeOf(Host{}),
		"MCPInfo": reflect.TypeOf(MCPInfo{}),
		"Finding": reflect.TypeOf(Finding{}),
	}
	if len(defs) != len(types) {
		t.Fatalf("schema has %d definitions, test covers %d", len(defs), len(types))
	}
	for name, typ := range types {
		def := defs[name].(map[string]any)
		var props []string
		for p := range def["properties"].(map[string]any) {
			props = append(props, p)
		}
		sort.Strings(props)
		var req []string
		if r, ok := def["required"].([]string); ok {
			req = append(req, r...)
		}
		sort.Strings(req)
		wantAll, wantReq := jsonFields(typ)
		if !reflect.DeepEqual(props, wantAll) {
			t.Errorf("%s properties = %v, json tags = %v", name, props, wantAll)
		}
		if !reflect.DeepEqual(req, wantReq) {
			t.Errorf("%s required = %v, tags without omitempty = %v", name, req, wantReq)
		}
	}
}
