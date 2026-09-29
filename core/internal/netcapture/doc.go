// Package netcapture provides the concrete runner.NetworkCapture
// adapters a script run observes HTTP traffic through: NullCapture
// for when no proxy is configured, and FileCapture for reading
// recorded traffic from a JSON-lines file. New dispatches on a Config
// to construct whichever adapter a host's proxy configuration names,
// so a composition root wires one call instead of choosing a concrete
// type itself.
package netcapture
