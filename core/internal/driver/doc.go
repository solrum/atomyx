// Package driver defines the platform-neutral Driver contract: the
// primitive device surface a concrete adapter (Android, iOS, a
// future platform) implements as a thin wrapper over its native
// control channel. Selector resolution, scroll-into-view,
// obscurement detection, retry, and any other cross-platform
// ergonomic live above this layer, never inside an adapter.
//
// Every Driver method that performs I/O or can be interrupted takes
// a leading context.Context. Canceling it must cause the call to
// return promptly with the context's cancellation error. A method
// with nothing to cancel (a synchronous accessor) takes no context.
package driver
