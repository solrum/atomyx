// Package lease gives each physical device at most one driving Atomyx
// process at a time. A lease is an exclusive flock on a per-device
// lock file, keyed by the device's canonical hardware identity, so the
// same phone reached over USB and over Wi-Fi maps to one lease.
//
// The kernel owns liveness. The flock is held on an open file
// descriptor for the process lifetime and disappears when the process
// exits for any reason, including SIGKILL, so a crashed holder never
// leaves a stale lock. The lock file also carries holder metadata
// (pid, command, role, run id, socket, start time, idle time) for
// display only. Nothing reads that metadata to make a control
// decision.
//
// Invariants callers rely on:
//
//   - Release unlinks the lock file while still holding the flock.
//     Every opener therefore compares its descriptor's identity with
//     the path's after the flock, at acquire time, on every Recheck,
//     and in Probe, and restarts when the file was replaced.
//   - A probe takes the same real flock as an acquirer but never
//     creates or writes the lock file, and never reports free while a
//     new holder owns a replacement file.
//   - A lock file is always opened through os.OpenFile so the
//     close-on-exec flag applies. A raw syscall.Open is rejected by
//     the check-no-raw-syscall-open gate.
package lease
