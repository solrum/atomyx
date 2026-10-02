package mcp

import (
	"strings"
	"testing"

	"dev.atomyx/internal/orchestra"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func maskedText(t *testing.T, doc string) string {
	t.Helper()
	result := &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: doc}}}
	maskSecureResult(result)
	return result.Content[0].(*mcp.TextContent).Text
}

func TestMaskSecureResult_MasksOnlySecureObjectsAndKeepsOrder(t *testing.T) {
	doc := `{
  "nodes": [
    {
      "role": "text-field",
      "text": "alice",
      "label": "User"
    },
    {
      "role": "text-field",
      "id": "pw",
      "text": "hunter2",
      "label": "Password",
      "value": "hunter2",
      "secure": true
    }
  ]
}`
	want := strings.NewReplacer(`"hunter2"`, `"`+orchestra.SecureMask+`"`).Replace(doc)

	if got := maskedText(t, doc); got != want {
		t.Fatalf("masked document differs:\n got: %s\nwant: %s", got, want)
	}
}

func TestMaskSecureResult_LeavesUnmarkedAndNonJSONBlocksUntouched(t *testing.T) {
	for _, doc := range []string{
		`{"text": "visible", "secure": false}`,
		`{"found": false}`,
		`not json "secure": true`,
	} {
		if got := maskedText(t, doc); got != doc {
			t.Fatalf("block %q changed to %q", doc, got)
		}
	}
}

func TestMaskSecureResult_KeepsEmptyValuesAndMasksNestedObjects(t *testing.T) {
	got := maskedText(t, `{"outer":{"inner":[{"text":"","value":"s3","secure":true}]}}`)

	if strings.Contains(got, "s3") || !strings.Contains(got, `"text": ""`) {
		t.Fatalf("got %s, want the value masked and the empty text kept", got)
	}
}
