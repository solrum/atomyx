package attach

import (
	"context"
	"net"
	"testing"
)

const replyBeforeEndRounds = 200

func TestCallReturnsAReplyThatArrivedBeforeTheConnectionEnded(t *testing.T) {
	beforeReplyWait = func(c *Client) { <-c.done }
	t.Cleanup(func() { beforeReplyWait = nil })

	for round := range replyBeforeEndRounds {
		holderEnd, clientEnd := net.Pipe()
		go func() {
			defer func() { _ = holderEnd.Close() }()
			request, err := NewFrameReader(holderEnd).ReadFrame()
			if err != nil {
				return
			}
			response, err := NewResponse(request.ID, request.Type, nil)
			if err != nil {
				return
			}
			_ = WriteFrame(holderEnd, response)
		}()
		c := newClient(clientEnd, 0)

		err := c.Call(context.Background(), TypeStatus, nil, nil)
		_ = c.Close()

		if err != nil {
			t.Fatalf("round %d: call error = %v, want the reply that was delivered before the close", round, err)
		}
	}
}
