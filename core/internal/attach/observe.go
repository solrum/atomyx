package attach

import "context"

// observe answers a request of the observe group. Only the connection's
// read goroutine calls it, so the subscription field needs no lock.
func (c *conn) observe(ctx context.Context, h ObserveHandler, f Frame) bool {
	switch f.Type {
	case TypeTreeLatest, TypeScreenshotLatest, TypeTreeFresh:
		return c.pull(h, f)
	default:
		return c.subscribe(ctx, h, f)
	}
}

// pull answers a request whose response can be large. The response goes
// to the writer queue without waiting for the write, so a viewer that
// has read one response can always send the next request. A viewer that
// pipelines pulls without reading cannot stack screenshots in memory,
// because the queue's byte budget disconnects it.
func (c *conn) pull(h ObserveHandler, f Frame) bool {
	var data any
	switch f.Type {
	case TypeTreeLatest:
		data = h.TreeLatest()
	case TypeScreenshotLatest:
		data = h.ScreenshotLatest()
	default:
		result, err := h.TreeFresh(c.ctx)
		if err != nil {
			return c.fail(f, err)
		}
		data = result
	}
	return c.respond(f, data)
}

// subscribe starts a notice stream. A connection holds one subscription
// at a time, so a new request ends the previous stream first. The old
// pump has stopped before the response is queued, so no notice of the old
// stream follows the response, and the viewer's resume point is the only
// thing that decides what the new stream replays.
func (c *conn) subscribe(parent context.Context, h ObserveHandler, f Frame) bool {
	var request Subscribe
	if err := f.Decode(&request); err != nil {
		return c.fail(f, invalidFrame(err.Error()))
	}
	c.endSubscription()
	ctx, sub := newLiveSubscription(parent)
	notices, err := h.Subscribe(ctx, request.RunID, request.SinceSeq)
	if err != nil {
		sub.cancel()
		return c.fail(f, err)
	}
	if !c.respond(f, nil) {
		sub.cancel()
		return false
	}
	c.sub = sub
	c.srv.wg.Add(1)
	go c.pump(ctx, notices, sub.done)
	return true
}

// endSubscription stops the live subscription, if any, and waits for its
// pump to end.
func (c *conn) endSubscription() {
	if c.sub == nil {
		return
	}
	c.sub.cancel()
	<-c.sub.done
	c.sub = nil
}

// pump moves notices from a subscription to the writer queue. It never
// waits on the viewer, because enqueue closes a connection whose queue is
// full.
func (c *conn) pump(ctx context.Context, notices <-chan Notice, done chan<- struct{}) {
	defer c.srv.wg.Done()
	defer close(done)
	for {
		select {
		case <-ctx.Done():
			return
		case n, open := <-notices:
			if !open {
				return
			}
			f, err := NewNotice(n.Type, n.Data)
			if err != nil {
				c.close()
				return
			}
			if !c.send(f) {
				return
			}
		}
	}
}
