package redact

import (
	"reflect"
	"strings"
	"testing"
)

func TestStringMasksValuesAndTokens(t *testing.T) {
	cases := []struct {
		name   string
		values []string
		in     string
		want   string
	}{
		{"plain value", []string{"hunter2hunter2"}, "pw=hunter2hunter2 end", "pw=" + Mask + " end"},
		{"every occurrence", []string{"abcdef"}, "abcdef abcdef", Mask + " " + Mask},
		{"longest first", []string{"secret", "secret-with-suffix"}, "x secret-with-suffix y", "x " + Mask + " y"},
		{"empty value ignored", []string{""}, "unchanged", "unchanged"},
		{"duplicate values", []string{"abcdef", "abcdef"}, "abcdef", Mask},
		{"bearer token", nil, "Authorization: Bearer abc.def-123_XYZ", "Authorization: Bearer " + Mask},
		{"bearer lowercase", nil, "bearer abcdefgh12345", "bearer " + Mask},
		{"inspector header", nil, `X-Atomyx-Token: s3cr3t-value`, "X-Atomyx-Token: " + Mask},
		{"inspector header json", nil, `"x-atomyx-token":"tok123"`, `"x-atomyx-token":"` + Mask + `"`},
		{"long hex", nil, "key=0123456789abcdef0123456789abcdef end", "key=" + Mask + " end"},
		{"short hex kept", nil, "id 0123456789abcdef", "id 0123456789abcdef"},
		{"long base64url", nil, "t=Abcdefghijklmnopqrstuvwxyz0123456789_-", "t=" + Mask},
		{"long identifier without digits kept", nil, "TestSomethingVeryLongIdentifierNameHere", "TestSomethingVeryLongIdentifierNameHere"},
		{"uuid kept", nil, "sim 3F2A6C1E-9B4D-4E7A-8C21-0D5E7F9A1B2C", "sim 3F2A6C1E-9B4D-4E7A-8C21-0D5E7F9A1B2C"},
		{"plain prose kept", nil, "tap the login button", "tap the login button"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := New(c.values).String(c.in); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestBytesMatchesString(t *testing.T) {
	r := New([]string{"abcdef"})
	if got := string(r.Bytes([]byte("x abcdef"))); got != "x "+Mask {
		t.Fatalf("got %q", got)
	}
}

func TestEnvValuesRules(t *testing.T) {
	environ := []string{
		"API_KEY=sk-live-123456",
		"SHORT=abc",
		"EXACT_MIN=abcdef",
		"BELOW_MIN=abcde",
		"FLAG=true",
		"OTHER=FALSE",
		"PORT=8080",
		"BIGNUM=1234567890",
		"HOME=/Users/someone",
		"CUSTOM_PATH=/opt/tools/bin",
		"USER=someoneelse",
		"TERM=xterm-256color",
		"NOEQUALS",
		"EMPTY=",
		"EQ_IN_VALUE=a=b=cdef",
	}
	want := []string{"sk-live-123456", "abcdef", "a=b=cdef"}
	if got := EnvValues(environ); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestEnvFileValues(t *testing.T) {
	content := strings.Join([]string{
		"# comment",
		"",
		"DEVICE_UDID=00008030-001A2B3C4D5E",
		"DEV_TEAM=\"ABCDE12345\"",
		"export BUNDLE_ID='com.example.app'",
		"SHORT=ab",
		"USER=someoneelse",
		"WIN=windowsvalue\r",
		"not a pair",
	}, "\n")
	want := []string{"00008030-001A2B3C4D5E", "ABCDE12345", "com.example.app", "windowsvalue"}
	if got := EnvFileValues([]byte(content)); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
