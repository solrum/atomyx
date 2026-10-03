package attach

// observe answers a request of the observe group. Only the connection's
// read goroutine calls it, so the subscribed flag needs no lock.
func (c *conn) observe(h ObserveHandler, f Frame) bool {
	switch f.Type {
	case TypeTreeLatest, TypeScreenshotLatest, TypeTreeFresh:
		return c.pull(h, f)
	default:
		return c.subscribe(h, f)
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

func (c *conn) subscribe(h ObserveHandler, f Frame) bool {
	if c.subscribed {
		return c.fail(f, invalidFrame("this connection is already subscribed"))
	}
	var request Subscribe
	if err := f.Decode(&request); err != nil {
		return c.fail(f, invalidFrame(err.Error()))
	}
	notices, err := h.Subscribe(c.ctx, request.RunID, request.SinceSeq)
	if err != nil {
		return c.fail(f, err)
	}
	c.subscribed = true
	if !c.respond(f, nil) {
		return false
	}
	c.srv.wg.Add(1)
	go c.pump(notices)
	return true
}

// pump moves notices from a subscription to the writer queue. It never
// waits on the viewer, because enqueue closes a connection whose queue is
// full.
func (c *conn) pump(notices <-chan Notice) {
	defer c.srv.wg.Done()
	for {
		select {
		case <-c.ctx.Done():
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
