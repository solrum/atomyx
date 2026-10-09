// Package shard spreads the entries of a suite over several devices. A
// coordinator feeds a pool of workers, one per device, from a single
// queue, so a fast device takes more entries than a slow one. Each worker
// is a separate process that runs the suite runner on its own device and
// reports each finished entry as one line of a small versioned protocol.
//
// The package depends on the process layer to start and stop workers and
// on nothing else in the project, so the command line reaches workers
// only through the Launcher port it defines.
package shard
