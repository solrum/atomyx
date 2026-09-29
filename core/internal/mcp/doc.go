// Package mcp builds the Atomyx Model Context Protocol server: the
// stdio-based process an AI agent connects to for tool-driven mobile
// test orchestration.
//
// This package owns server construction, tool registration, and
// stdio transport wiring. A tool handler validates its JSON
// arguments against its own JSON Schema before acting on them, since
// the SDK's manual AddTool path leaves argument validation to the
// handler.
package mcp
