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

// Handlers groups what a server's owner supplies. A nil group is not
// served: its requests are answered with UNSUPPORTED and its feature is
// left out of the hello answer.
type Handlers struct {
	Session SessionHandler
	Observe ObserveHandler
}

func (h Handlers) features() []string {
	features := []string{}
	if h.Observe != nil {
		features = append(features, FeatureObserve)
	}
	return features
}
