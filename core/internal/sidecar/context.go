package sidecar

// Context is what every feature receives at registration time.
// Deliberately narrow. A feature that needs more (a logger, a
// clock, a stores registry) takes it from its own constructor, not
// by growing this struct. Adding a field means a cross-cutting
// capability has to reach every feature; that is a separate
// decision.
type Context struct {
	Dispatcher *Dispatcher
	Events     *EventBus
	Session    *Session
}

// Handle is what a feature registrar returns to the composition
// root. Dispose, when non-nil, is called on shutdown to release the
// feature's resources.
type Handle struct {
	Dispose func() error
}

// Registrar wires one feature's handlers and state into ctx and
// returns a Handle the composition root uses for teardown.
type Registrar func(ctx *Context) Handle
