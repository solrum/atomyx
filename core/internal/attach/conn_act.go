package attach

import "dev.atomyx/internal/diag"

// serveAct runs an act.step request on its own goroutine, so the read
// loop keeps reading while the step runs and sees the viewer disconnect.
// The disconnect ends the connection context, which cancels the step.
// A connection runs one act at a time, and a second one that arrives
// meanwhile is refused as busy. Responses still go through the writer
// queue, which is safe from any goroutine.
func (c *conn) serveAct(f Frame) bool {
	if !c.actRunning.CompareAndSwap(false, true) {
		return c.fail(f, diag.NewHolderBusy("an act is already running on this connection"))
	}
	c.srv.wg.Add(1)
	go func() {
		defer c.srv.wg.Done()
		defer c.actRunning.Store(false)
		c.dispatch(c.ctx, f)
	}()
	return true
}
