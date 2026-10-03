package mcp

import (
	"encoding/json"

	"dev.atomyx/internal/session"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// interventionsBlock is the body of the text content block that carries
// the interventions an agent has not seen yet.
type interventionsBlock struct {
	Interventions []session.Intervention `json:"interventions"`
}

// attachInterventions appends entries to result as one extra text content
// block holding {"interventions":[{"kind","atMs","client"}]}, after the
// tool's own content and any notices, so a client that reads the first
// block sees the unchanged result. It adds nothing when entries is empty.
func attachInterventions(result *mcp.CallToolResult, entries []session.Intervention) {
	if result == nil || len(entries) == 0 {
		return
	}
	body, err := json.Marshal(interventionsBlock{Interventions: entries})
	if err != nil {
		// The block holds only strings and integers, so a marshal failure
		// can only be a bug in this file.
		panic(err)
	}
	result.Content = append(result.Content, &mcp.TextContent{Text: string(body)})
}
