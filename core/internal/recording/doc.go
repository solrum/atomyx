// Package recording owns the lifecycle of a screen recording tied to a
// run. It starts a recording for a run or for one attempt of a suite
// entry, stops it when the result is known, and keeps the video files
// in the run folder when the result failed, or discards them
// otherwise.
//
// The package depends on the device layer for the recorder port and on
// the run folder package for the place the files are kept. Only the
// command line and the MCP server may import it, so scenario and the
// layers below it reach recording through callbacks supplied by their
// host.
package recording
