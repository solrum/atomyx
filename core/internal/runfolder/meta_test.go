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
		{"running", "", ""},
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

type fakeLiveness map[string]bool

func (f fakeLiveness) IsLive(id string) bool { return f[id] }

func TestLegacyStatusRoundTrips(t *testing.T) {
	for _, status := range []string{"passed", "failed", "error", "cancelled", "running"} {
		result, outcome, ok := FromLegacyStatus(status)
		if !ok {
			t.Fatalf("%s: unknown", status)
		}
		meta := Meta{RunID: "r", Result: result, Outcome: outcome}
		if status != "running" {
			done := int64(2)
			meta.CompletedAt = &done
		}
		if got := LegacyStatusOf(meta.Settle(fakeLiveness{"r": true})); got != status {
			t.Errorf("%s: round trip through a live settle gave %q", status, got)
		}
	}
}

func TestLegacyStatusOfCoversEveryOutcome(t *testing.T) {
	want := map[Outcome]string{
		OutcomePassed: "passed", OutcomeFailed: "failed", OutcomeError: "error",
		OutcomeCancelled: "cancelled", OutcomeDeviceLost: "cancelled", OutcomeInterrupted: "cancelled",
	}
	for _, o := range Outcomes() {
		result, _ := ResultOf(o)
		done := int64(2)
		if got := LegacyStatusOf(Meta{CompletedAt: &done, Result: result, Outcome: o}); got != want[o] {
			t.Errorf("%s: got %q, want %q", o, got, want[o])
		}
	}
}

func TestSettleResolvesAnUnfinishedRunByLiveness(t *testing.T) {
	unfinished := Meta{RunID: "r", Result: ResultPassed, Outcome: OutcomePassed}

	live := unfinished.Settle(fakeLiveness{"r": true})
	if live.Result != "" || live.Outcome != "" || LegacyStatusOf(live) != "running" {
		t.Fatalf("live run: got %+v", live)
	}
	for name, l := range map[string]Liveness{"not live": fakeLiveness{}, "nil": nil} {
		got := unfinished.Settle(l)
		if got.Result != ResultCancelled || got.Outcome != OutcomeInterrupted || LegacyStatusOf(got) != "cancelled" {
			t.Fatalf("%s: got %+v", name, got)
		}
	}
	done := int64(9)
	finished := Meta{RunID: "r", CompletedAt: &done, Result: ResultFailed, Outcome: OutcomeError}
	if got := finished.Settle(fakeLiveness{"r": true}); got != finished {
		t.Fatalf("a completed run changed: %+v", got)
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
		"RunMeta":                reflect.TypeOf(Meta{}),
		"Device":                 reflect.TypeOf(Device{}),
		"Host":                   reflect.TypeOf(Host{}),
		"MCPInfo":                reflect.TypeOf(MCPInfo{}),
		"Finding":                reflect.TypeOf(Finding{}),
		"Recording":              reflect.TypeOf(Recording{}),
		"Intervention":           reflect.TypeOf(InterventionLine{}),
		"InterventionsTruncated": reflect.TypeOf(InterventionsTruncated{}),
		"StepLine":               reflect.TypeOf(StepLine{}),
	}
	delete(defs, "InterventionEntry")
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

func TestMetaRecordingRoundTripsAndIsOmittedWhenAbsent(t *testing.T) {
	bare, err := json.Marshal(Meta{SchemaVersion: 1, RunID: "r", Kind: KindScript})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(bare, []byte(`"recording"`)) {
		t.Fatalf("absent recording encoded: %s", bare)
	}
	var old Meta
	if err := json.Unmarshal(bare, &old); err != nil || old.Recording != nil {
		t.Fatalf("meta without recording = %+v, %v", old, err)
	}

	kept := Meta{Recording: &Recording{State: RecordingKept, Files: []string{"video-a-1.mp4"}}}
	data, err := json.Marshal(kept)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"recording":{"state":"kept","files":["video-a-1.mp4"]}`)) {
		t.Fatalf("encoding = %s", data)
	}
	failed := Meta{Recording: &Recording{State: RecordingError, Code: "X"}}
	data, _ = json.Marshal(failed)
	if !bytes.Contains(data, []byte(`"recording":{"state":"error","code":"X"}`)) {
		t.Fatalf("encoding = %s", data)
	}
}

func TestMetaSuiteCountsAreOmittedWhenZeroAndEncodedWhenSet(t *testing.T) {
	bare, err := json.Marshal(Meta{SchemaVersion: 1, RunID: "r", Kind: KindSuite})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"flakyEntries"`, `"quarantinedFailures"`} {
		if bytes.Contains(bare, []byte(key)) {
			t.Errorf("a zero count encoded %s: %s", key, bare)
		}
	}

	data, err := json.Marshal(Meta{FlakyEntries: 2, QuarantinedFailures: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"flakyEntries":2`)) || !bytes.Contains(data, []byte(`"quarantinedFailures":1`)) {
		t.Fatalf("encoding = %s", data)
	}
	var back Meta
	if err := json.Unmarshal(data, &back); err != nil || back.FlakyEntries != 2 || back.QuarantinedFailures != 1 {
		t.Fatalf("round trip = %+v, %v", back, err)
	}
	var old Meta
	if err := json.Unmarshal(bare, &old); err != nil || old.FlakyEntries != 0 || old.QuarantinedFailures != 0 {
		t.Fatalf("meta without counts = %+v, %v", old, err)
	}
}

func TestMetaParentRunIDIsOmittedWhenEmptyAndRoundTripsWhenSet(t *testing.T) {
	bare, err := json.Marshal(Meta{SchemaVersion: 1, RunID: "r", Kind: KindSuite})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(bare, []byte(`"parentRunId"`)) {
		t.Errorf("an empty parent id encoded: %s", bare)
	}
	data, err := json.Marshal(Meta{ParentRunID: "20261009-101500-shard-ab12"})
	if err != nil {
		t.Fatal(err)
	}
	var back Meta
	if err := json.Unmarshal(data, &back); err != nil || back.ParentRunID != "20261009-101500-shard-ab12" {
		t.Fatalf("round trip = %+v, %v", back, err)
	}
}

func TestRecordAgentKeepsTheHostAndSkipsZeroValues(t *testing.T) {
	meta := Meta{Host: &Host{AtomyxVersion: "0.1.0"}}
	RecordAgent(&meta, 0, "")
	if *meta.Host != (Host{AtomyxVersion: "0.1.0"}) {
		t.Fatalf("host = %+v, want it unchanged for zero values", *meta.Host)
	}
	RecordAgent(&meta, 2, "1.4")
	if *meta.Host != (Host{AtomyxVersion: "0.1.0", ProtocolVersion: 2, AgentVersion: "1.4"}) {
		t.Fatalf("host = %+v, want the agent versions beside the atomyx version", *meta.Host)
	}
	bare := Meta{}
	RecordAgent(&bare, 0, "")
	if bare.Host != nil {
		t.Fatalf("host = %+v, want nil for zero values", bare.Host)
	}
	RecordAgent(&bare, 1, "")
	if bare.Host == nil || bare.Host.ProtocolVersion != 1 {
		t.Fatalf("host = %+v, want protocol 1", bare.Host)
	}
}

func TestMetaErrorCodeIsOmittedWhenEmptyAndRoundTrips(t *testing.T) {
	plain, err := json.Marshal(Meta{RunID: "r"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plain), "errorCode") {
		t.Fatalf("empty error code was written: %s", plain)
	}
	raw, err := json.Marshal(Meta{RunID: "r", ErrorCode: "AGENT_UNREACHABLE"})
	if err != nil {
		t.Fatal(err)
	}
	var back Meta
	if err := json.Unmarshal(raw, &back); err != nil || back.ErrorCode != "AGENT_UNREACHABLE" {
		t.Fatalf("round trip = %+v, %v", back, err)
	}
}
