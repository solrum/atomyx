package mcp

import "testing"

func TestMCPRunMetaCarriesAnMCPTestKey(t *testing.T) {
	meta := mcpRunMeta("checkout smoke", nil, nil)
	if meta.TestKey != "mcp:checkout smoke" {
		t.Fatalf("TestKey = %q, want mcp:checkout smoke", meta.TestKey)
	}
}
