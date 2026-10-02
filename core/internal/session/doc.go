// Package session is the one path by which a process takes ownership of
// a device. Open resolves the device's canonical identity, acquires its
// lease, ends any runner a crashed earlier holder left behind, and only
// then builds the driver, so no process connects to a device that
// another Atomyx process is driving.
//
// A Session owns three things for its lifetime: the lease, the built
// driver with its Orchestra, and a watcher that notices when the lease
// file is replaced under it. Close releases them in that order of
// dependency, disconnecting the driver, which stops a runner the
// factory launched, before giving up the lease.
//
// The CLI commands run, suite and doctor, and the MCP server's device
// selection, all go through Open. Nothing else in the module builds a
// driver for a device it does not already hold a lease on.
package session
