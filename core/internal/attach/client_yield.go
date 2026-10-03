package attach

import (
	"context"
	"slices"

	"dev.atomyx/internal/diag"
)

// YieldResult reports how far a yield request got, because a caller must
// tell a request the holder never saw from one it may have acted on.
type YieldResult struct {
	// Sent is true once control.yield was written toward the holder. A
	// failure before that, such as a refused dial or a failed hello, leaves
	// it false, so the holder was never asked.
	Sent bool
	// Acknowledged is true once the holder replied ok, which means it has
	// started to give its device up.
	Acknowledged bool
}

// RequestYield asks the holder at cfg.Path to give up its device to the
// process described by command, a short human readable string such as
// "atomyx run login.yml". It dials, completes the hello exchange, sends
// control.yield, and then waits for the holder to end the connection, so
// a nil error means the holder replied ok and has closed its session
// side of the socket. The caller acquires the device afterwards.
//
// The result says how far the request got even when the error is not nil,
// so a ctx that ends after the holder's ok still reports Acknowledged.
//
// Failures:
//   - a holder whose hello lacks the control feature returns a
//     diag.CodeUnsupported error without sending anything
//   - a holder that cannot yield now returns the *RemoteError it sent,
//     for example diag.CodeHolderBusy, with Sent set and Acknowledged not
//   - ctx ending before the connection does returns ctx.Err()
//
// Dial errors are returned as Dial reports them. cfg.Features is
// extended with FeatureControl.
func RequestYield(ctx context.Context, cfg ClientConfig, command string) (YieldResult, error) {
	var result YieldResult
	cfg.Features = append(slices.Clone(cfg.Features), FeatureControl)
	c, err := Dial(ctx, cfg)
	if err != nil {
		return result, err
	}
	defer func() { _ = c.Close() }()
	if !slices.Contains(c.Features(), FeatureControl) {
		return result, diag.NewUnsupported(TypeControlYield)
	}
	result.Sent = true
	if err := c.Call(ctx, TypeControlYield, ControlYield{Command: command}, nil); err != nil {
		return result, err
	}
	result.Acknowledged = true
	select {
	case <-c.Done():
		return result, nil
	case <-ctx.Done():
		return result, ctx.Err()
	}
}
