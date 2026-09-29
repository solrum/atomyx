// Package sidecar implements the host-side control process: a
// newline-delimited JSON-RPC bridge over stdin/stdout, the method
// dispatcher that routes a request to a registered handler, the
// per-process session that tracks the currently selected device, and
// the event bus that carries out-of-band server-to-client
// notifications.
//
// This package knows nothing about individual device platforms or
// script execution; it is the transport and routing substrate that
// feature handlers register against.
package sidecar
