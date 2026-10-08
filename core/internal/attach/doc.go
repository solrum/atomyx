// Package attach is the protocol a process that holds a device speaks to
// viewers that attach to it over a unix socket, and the server and client
// that carry it.
//
// Framing: one JSON object per line. A request carries a positive id and
// a type. A response echoes both and reports ok, with a code and message
// when it failed. A notice has neither id nor ok and travels from the
// holder to a subscribed viewer without being asked for.
//
// Versioning: the first request of every connection is session.hello with
// the range of major versions the client speaks. Additive fields and new
// types keep the major version. A removed or reshaped field raises it.
// The holder answers with the version it chose, or UNSUPPORTED_VERSION
// when the ranges do not overlap.
//
// Message groups: session (hello, status, bye), observe (subscribe,
// tree.latest, screenshot.latest, tree.fresh, plus the notices that
// subscribe starts), control (yield, served only by a holder that
// advertises the control feature) and debug (breakpoints.set, pause,
// resume, step, variables.get, plus act.step, served only by a holder that
// advertises the debug feature). An unknown type is answered with
// UNSUPPORTED and the connection stays open.
//
// The package depends only on wire and diag, so the holder's own session
// types reach it through interfaces its owner supplies. Screenshots
// cross the socket unmasked, and the socket accepts only peers with the
// same uid as the holder.
package attach
