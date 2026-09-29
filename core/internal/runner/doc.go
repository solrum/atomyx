// Package runner defines the command execution contract, made up of
// CommandContext, CommandResult, and CommandDefinition, plus the
// ScriptRunner that dispatches a parsed ScriptDefinition's steps
// against that contract, one step at a time, failing fast on the
// first step that does not succeed.
package runner
