package attach

import (
	"context"
	"slices"

	"dev.atomyx/internal/diag"
)

// RequestYield asks the holder at cfg.Path to give up its device to the
// process described by command, a short human readable string such as
// "atomyx run login.yml". It dials, completes the hello exchange, sends
// control.yield, and then waits for the holder to end the connection, so
// a nil return means the holder replied ok and has closed its session
// side of the socket. The caller acquires the device afterwards.
//
// Failures:
//   - a holder whose hello lacks the control feature returns a
//     diag.CodeUnsupported error without sending anything
//   - a holder that cannot yield now returns the *RemoteError it sent,
//     for example diag.CodeHolderBusy
//   - ctx ending before the connection does returns ctx.Err()
//
// Dial errors are returned as Dial reports them. cfg.Features is
// extended with FeatureControl.
func RequestYield(ctx context.Context, cfg ClientConfig, command string) error {
	cfg.Features = append(slices.Clone(cfg.Features), FeatureControl)
	c, err := Dial(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	if !slices.Contains(c.Features(), FeatureControl) {
		return diag.NewUnsupported(TypeControlYield)
	}
	if err := c.Call(ctx, TypeControlYield, ControlYield{Command: command}, nil); err != nil {
		return err
	}
	select {
	case <-c.Done():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
