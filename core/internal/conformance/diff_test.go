package conformance

import "testing"

func TestDiffJSON_Equal(t *testing.T) {
	report, equal, err := DiffJSON(
		[]byte(`{"a":1,"b":[1,2,3]}`),
		[]byte(`{"b":[1,2,3],"a":1}`),
	)
	if err != nil {
		t.Fatalf("DiffJSON returned error: %v", err)
	}
	if !equal {
		t.Fatalf("expected equal values, got report:\n%s", report)
	}
	if report != "" {
		t.Fatalf("expected empty report for equal values, got:\n%s", report)
	}
}

func TestDiffJSON_Mismatch(t *testing.T) {
	report, equal, err := DiffJSON(
		[]byte(`{"version":1}`),
		[]byte(`{"version":2}`),
	)
	if err != nil {
		t.Fatalf("DiffJSON returned error: %v", err)
	}
	if equal {
		t.Fatal("expected mismatched values to be unequal")
	}
	if report == "" {
		t.Fatal("expected a non-empty report for mismatched values")
	}
}

func TestDiffJSON_InvalidJSON(t *testing.T) {
	if _, _, err := DiffJSON([]byte(`{`), []byte(`{}`)); err == nil {
		t.Fatal("expected an error for invalid want JSON")
	}
	if _, _, err := DiffJSON([]byte(`{}`), []byte(`{`)); err == nil {
		t.Fatal("expected an error for invalid got JSON")
	}
}

func TestDiffRunnerJSON_SameCodeDifferentWordingPasses(t *testing.T) {
	want := `{"ok":false,"steps":[{"stepIndex":0,"command":"pointer","ok":false,
		"detail":"POINTER_MULTI_NOT_SUPPORTED: On iOS, upgrade Xcode. On Android, not yet available.","durationMs":1}]}`
	got := `{"ok":false,"steps":[{"stepIndex":0,"command":"pointer","ok":false,
		"detail":"POINTER_MULTI_NOT_SUPPORTED: multi-pointer gestures are not supported by the active driver.","durationMs":1}]}`

	report, equal, err := DiffRunnerJSON([]byte(want), []byte(got))
	if err != nil {
		t.Fatalf("DiffRunnerJSON returned error: %v", err)
	}
	if !equal {
		t.Fatalf("expected wording-only divergence to compare equal, got report:\n%s", report)
	}
}

func TestDiffRunnerJSON_DifferentCodeStillFails(t *testing.T) {
	want := `{"ok":false,"steps":[{"stepIndex":0,"command":"pointer","ok":false,
		"detail":"POINTER_MULTI_NOT_SUPPORTED: not supported.","durationMs":1}]}`
	got := `{"ok":false,"steps":[{"stepIndex":0,"command":"pointer","ok":false,
		"detail":"POINTER_FORM_CONFLICT: not supported.","durationMs":1}]}`

	report, equal, err := DiffRunnerJSON([]byte(want), []byte(got))
	if err != nil {
		t.Fatalf("DiffRunnerJSON returned error: %v", err)
	}
	if equal {
		t.Fatal("expected a divergent error code to still fail the diff")
	}
	if report == "" {
		t.Fatal("expected a non-empty report for a divergent error code")
	}
}

func TestDiffRunnerJSON_SuccessDetailStaysStrict(t *testing.T) {
	want := `{"ok":true,"steps":[{"stepIndex":0,"command":"extract","ok":true,
		"detail":"Extracted: alpha, beta","durationMs":1}]}`
	got := `{"ok":true,"steps":[{"stepIndex":0,"command":"extract","ok":true,
		"detail":"Extracted: alpha, gamma","durationMs":1}]}`

	report, equal, err := DiffRunnerJSON([]byte(want), []byte(got))
	if err != nil {
		t.Fatalf("DiffRunnerJSON returned error: %v", err)
	}
	if equal {
		t.Fatal("expected a divergent successful-step detail to still fail the diff")
	}
	if report == "" {
		t.Fatal("expected a non-empty report for a divergent successful-step detail")
	}
}

func TestNormalizeErrorDetailToken(t *testing.T) {
	cases := []struct {
		name   string
		detail string
		want   string
	}{
		{"code and guidance", "POINTER_MULTI_NOT_SUPPORTED: multi-pointer gestures are not supported.", "POINTER_MULTI_NOT_SUPPORTED:"},
		{"exception prefix", "Exception: element not found", "Exception:"},
		{"no colon left unchanged", "Element is visible", "Element is visible"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := normalizeErrorDetailToken(c.detail); got != c.want {
				t.Fatalf("normalizeErrorDetailToken(%q) = %q, want %q", c.detail, got, c.want)
			}
		})
	}
}
