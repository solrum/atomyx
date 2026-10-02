package orchestra

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

func TestPattern_UnmarshalLiteral(t *testing.T) {
	var p Pattern
	if err := json.Unmarshal([]byte(`"login_btn"`), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !p.Matches("login_btn") || p.Matches("other") {
		t.Fatalf("literal pattern matched incorrectly")
	}
}

func TestPattern_UnmarshalRegex(t *testing.T) {
	var p Pattern
	if err := json.Unmarshal([]byte(`{"regex":"^cancel$","flags":"i"}`), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !p.Matches("Cancel") || p.Matches("cancellation") {
		t.Fatalf("regex pattern matched incorrectly")
	}
}

func TestPattern_UnmarshalRegexNoFlags(t *testing.T) {
	var p Pattern
	if err := json.Unmarshal([]byte(`{"regex":"^[0-9]+$"}`), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !p.Matches("12345") || p.Matches("12a45") {
		t.Fatalf("regex pattern matched incorrectly")
	}
}

func TestPattern_MarshalRoundTrip(t *testing.T) {
	lit := LiteralPattern("hello")
	data, err := json.Marshal(lit)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) != `"hello"` {
		t.Fatalf("got %s, want \"hello\"", data)
	}

	re, err := compileJSRegex("^a+$", "i")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	regexPattern := RegexPattern(re)
	data, err = json.Marshal(regexPattern)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded Pattern
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal round trip: %v", err)
	}
	if !decoded.Matches("aaa") {
		t.Fatalf("round-tripped regex pattern lost its match behavior")
	}
}

func TestPattern_UnsupportedFlag(t *testing.T) {
	if _, err := compileJSRegex("abc", "z"); err == nil {
		t.Fatal("expected an error for unsupported flag")
	}
}

func TestPattern_UnsupportedJSRegexConstructsFailLoud(t *testing.T) {
	cases := []struct {
		name   string
		source string
	}{
		{"lookahead", "foo(?=bar)"},
		{"negative lookahead", "foo(?!bar)"},
		{"lookbehind", "(?<=foo)bar"},
		{"negative lookbehind", "(?<!foo)bar"},
		{"backreference", `(\w+)\1`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := compileJSRegex(tc.source, "")
			if err == nil {
				t.Fatalf("compileJSRegex(%q) succeeded, want an explicit unsupported-construct error", tc.source)
			}
			if !strings.Contains(err.Error(), strconv.Quote(tc.source)) {
				t.Fatalf("error %q does not name the offending pattern %q", err.Error(), tc.source)
			}
		})
	}
}

func TestPattern_SupportedJSRegexConstructsStillCompile(t *testing.T) {
	cases := []string{
		`^[0-9]+$`,
		`(?:abc)+`,
		`a\d\s\w+`,
		`[a-z()=!]+`,
	}
	for _, source := range cases {
		if _, err := compileJSRegex(source, ""); err != nil {
			t.Fatalf("compileJSRegex(%q) failed unexpectedly: %v", source, err)
		}
	}
}

func TestPattern_LiteralReportsTheStringOfALiteralPatternOnly(t *testing.T) {
	lit := LiteralPattern("Sign in")
	if got, ok := lit.Literal(); !ok || got != "Sign in" {
		t.Errorf("literal pattern: Literal() = %q, %v, want %q, true", got, ok, "Sign in")
	}
	var re Pattern
	if err := re.UnmarshalJSON([]byte(`{"regex":"^Sign"}`)); err != nil {
		t.Fatal(err)
	}
	if got, ok := re.Literal(); ok || got != "" {
		t.Errorf("regex pattern: Literal() = %q, %v, want empty, false", got, ok)
	}
}
