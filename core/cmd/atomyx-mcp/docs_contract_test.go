package main

import (
	"os"
	"regexp"
	"strconv"
	"testing"

	"dev.atomyx/internal/session"
)

const (
	msPerSecond int64 = 1_000
	msPerMinute int64 = 60_000
)

func TestDocumentedDefaultTimeoutsMatchTheCode(t *testing.T) {
	raw, err := os.ReadFile("../../../docs/mcp-tools.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	claims := []struct {
		name   string
		re     string
		unitMs int64
		want   int64
	}{
		{"acquisition budget", `(?i)acquisition has its own budget of (\d+) seconds`, msPerSecond, session.DefaultDeviceAcquireTimeoutMs},
		{"runner build timeout", `(?i)\bbuild[^.\n]*?(\d+) seconds`, msPerSecond, session.DefaultRunnerBuildTimeoutMs},
		{"MCP idle timeout", `(?i)MCP idle timeout, (\d+) minutes`, msPerMinute, session.DefaultMcpIdleTimeoutMs},
		{"open-run idle timeout", `(?i)open-run idle timeout, (\d+) minutes`, msPerMinute, session.DefaultOpenRunIdleTimeoutMs},
	}
	for _, c := range claims {
		match := regexp.MustCompile(c.re).FindStringSubmatch(doc)
		if match == nil {
			t.Errorf("%s: docs/mcp-tools.md does not state a default", c.name)
			continue
		}
		n, err := strconv.ParseInt(match[1], 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		if got := n * c.unitMs; got != c.want {
			t.Errorf("%s: docs say %d ms, code says %d ms", c.name, got, c.want)
		}
	}
}
