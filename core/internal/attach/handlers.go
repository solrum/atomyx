package attach

import "context"

// SessionHandler answers the session group requests that need the
// holder's own state.
type SessionHandler interface {
	// Status reports the holder, its device and its state.
	Status() Status
}

// Notice is one message a subscription pushes to a viewer. Type is one of
// the Notice constants and Data is its payload struct.
type Notice struct {
	Type string
	Data any
}

// ObserveHandler answers the observe group requests from what the holder
// has captured.
type ObserveHandler interface {
	// TreeLatest returns the newest masked tree.
	TreeLatest() TreeResult
	// ScreenshotLatest returns the newest screenshot.
	ScreenshotLatest() ScreenshotResult
	// TreeFresh captures a matched tree and screenshot, or fails with
	// the error that says why the holder refuses, for example
	// diag.CodeHolderBusy. It must return once ctx ends.
	TreeFresh(ctx context.Context) (FreshResult, error)
	// Subscribe starts a notice stream that resumes after line sinceSeq
	// of run runID. The stream ends when the handler closes the channel
	// or ctx ends.
	Subscribe(ctx context.Context, runID string, sinceSeq int64) (<-chan Notice, error)
}

// ControlHandler answers the control group requests. A holder that wants
// to give the device to a borrower supplies one.
type ControlHandler interface {
	// Yield decides whether the holder gives the device to the process
	// described by borrower. It is called on the connection's read
	// goroutine, so it must return promptly and decide under the host's
	// own lock.
	//
	// A non-nil error refuses, and the server sends it as the response,
	// for example diag.CodeHolderBusy while the holder is working. The
	// connection stays usable.
	//
	// A nil error accepts: the host has committed to release the device.
	// The server then writes the ok response and runs afterReply exactly
	// once on a new goroutine, after that response has been written to
	// the connection, or when the connection ended first. afterReply may
	// be nil. It may close the whole session, including the server that
	// calls it, because it never runs on the connection's goroutines.
	Yield(ctx context.Context, borrower string) (afterReply func(), err error)
}

// ViewerInfo identifies one attached viewer. ID is unique among the
// connections a server has accepted, and Client is the name the viewer
// sent in its hello request.
type ViewerInfo struct {
	ID     uint64
	Client string
}

// ViewerHandler is told when viewers attach and detach, so the holder can
// show who is watching. Both methods run on the connection's own
// goroutine, so they must return promptly.
type ViewerHandler interface {
	// ViewerAttached is called once per connection, after its hello
	// request succeeded.
	ViewerAttached(info ViewerInfo)
	// ViewerDetached is called once for each ViewerAttached, when the
	// connection ends for any reason.
	ViewerDetached(info ViewerInfo)
}

// Handlers groups what a server's owner supplies. A nil group is not
// served: its requests are answered with UNSUPPORTED and its feature is
// left out of the hello answer.
type Handlers struct {
	Session SessionHandler
	Observe ObserveHandler
	// Control is optional. A nil value leaves the control feature out of
	// the hello answer.
	Control ControlHandler
	// Viewers is optional. A nil value skips the attach and detach calls.
	Viewers ViewerHandler
}

func (h Handlers) features() []string {
	features := []string{}
	if h.Observe != nil {
		features = append(features, FeatureObserve, FeatureObserveState)
	}
	if h.Control != nil {
		features = append(features, FeatureControl)
	}
	return features
}
