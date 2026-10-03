package attach

import (
	"context"
	"fmt"
)

// yield answers control.yield. An accepted yield queues the response with
// the host's callback attached, so the callback runs only after the
// writer has put the response on the connection.
func (c *conn) yield(ctx context.Context, h ControlHandler, f Frame) bool {
	var request ControlYield
	if err := f.Decode(&request); err != nil {
		return c.fail(f, invalidFrame(err.Error()))
	}
	if request.Command == "" {
		return c.fail(f, invalidFrame("control.yield needs a command"))
	}
	if len(request.Command) > MaxBorrowerCommandBytes {
		return c.fail(f, invalidFrame(fmt.Sprintf("the command is longer than %d bytes", MaxBorrowerCommandBytes)))
	}
	after, err := h.Yield(ctx, request.Command)
	if err != nil {
		return c.fail(f, err)
	}
	if after != nil {
		c.afterMu.Lock()
		c.after = append(c.after, after)
		c.afterMu.Unlock()
	}
	response, encodeErr := NewResponse(f.ID, f.Type, nil)
	if encodeErr != nil {
		c.close()
		return false
	}
	return c.sendItem(response, true)
}
