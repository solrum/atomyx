package session

import "context"

// Yielder lets the host that owns a session give the device to another
// process that asked for it over the session's attach socket.
//
// A host injects one through Request.Yielder only when it can release
// the device while idle. A session without one does not advertise the
// control feature, and a yield request to it is answered UNSUPPORTED.
type Yielder interface {
	// Yield decides, under the host's own lock, whether to give the device
	// to the process described by borrower, a short human readable command
	// such as "atomyx run login.yml". It runs on the connection goroutine
	// that read the request, so it must not block on the session's close.
	//
	// A non-nil error refuses, and it reaches the borrower as the
	// response. The host returns a diag.CodeHolderBusy error while a call,
	// a capture or a run is in flight. The connection stays usable.
	//
	// A nil error accepts: the host has committed to release the device,
	// and must record that decision before it returns so no other path
	// closes the session as well. The attach server then writes the ok
	// response and runs afterReply exactly once on a goroutine of its
	// own, after the response has been written to the connection, or
	// when the connection ended before that. afterReply may be nil. It
	// may close the whole session, including the attach server, because
	// it never runs on a goroutine the server waits for.
	Yield(ctx context.Context, borrower string) (afterReply func(), err error)
}
