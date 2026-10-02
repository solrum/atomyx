package mcp

import (
	"context"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// appRelaunchedNotice is the text an agent receives on the call that took
// its device again and had to launch the app afresh.
const appRelaunchedNotice = "Notice: the device was taken again after an idle release and the app was " +
	"relaunched on a new runner, so its in-memory state was lost. Check the screen before continuing."

// callNotices collects the agent-visible notices raised while one tool
// call runs. The middleware attaches them to that call's result, so no
// handler formats or forwards them.
type callNotices struct {
	mu    sync.Mutex
	texts []string
}

type callNoticesKey struct{}

func withCallNotices(ctx context.Context) (context.Context, *callNotices) {
	notices := &callNotices{}
	return context.WithValue(ctx, callNoticesKey{}, notices), notices
}

// addNotice records text for the call ctx belongs to. A ctx outside a
// tool call makes it a no-op.
func addNotice(ctx context.Context, text string) {
	notices, ok := ctx.Value(callNoticesKey{}).(*callNotices)
	if !ok {
		return
	}
	notices.mu.Lock()
	defer notices.mu.Unlock()
	notices.texts = append(notices.texts, text)
}

// attachTo appends each notice to result as an extra text content block
// after the tool's own content, so a client that reads the first block
// sees the unchanged result.
func (n *callNotices) attachTo(result *mcp.CallToolResult) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if result == nil {
		return
	}
	for _, text := range n.texts {
		result.Content = append(result.Content, &mcp.TextContent{Text: text})
	}
}
