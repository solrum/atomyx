// Package debugrun adapts a script run to the debug ports of a device
// session. One Adapter implements both runner.StepGate, which holds the
// run at a breakpoint, and session.Debugger, which a viewer drives while
// the run is paused. The command line and the MCP server both host
// scripts, so they share this adapter instead of each carrying a copy.
package debugrun
