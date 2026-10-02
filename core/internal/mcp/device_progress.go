package mcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// acquirePhase is one step of bringing a device up, reported to a
// client that asked for progress on the call that triggered it.
type acquirePhase struct {
	step    float64
	message string
}

const acquirePhaseCount = 3

var (
	phaseBuilding  = acquirePhase{step: 1, message: "Preparing the device runner, building it when it is missing or stale"}
	phaseLaunching = acquirePhase{step: 2, message: "Launching the device runner"}
	phaseConnected = acquirePhase{step: 3, message: "Device connected"}
)

// phaseReporter delivers acquisition phases to the client that issued
// the call.
type phaseReporter func(ctx context.Context, phase acquirePhase)

type phaseReporterKey struct{}

func withPhaseReporter(ctx context.Context, report phaseReporter) context.Context {
	return context.WithValue(ctx, phaseReporterKey{}, report)
}

// reportPhase delivers phase to the reporter ctx carries. A ctx with
// none, which is every call whose client sent no progress token, makes
// it a no-op.
func reportPhase(ctx context.Context, phase acquirePhase) {
	if report, ok := ctx.Value(phaseReporterKey{}).(phaseReporter); ok {
		report(ctx, phase)
	}
}

// progressReporter sends each phase as a progress notification tagged
// with token. A client that stopped listening is not an error the call
// should fail on, so a failed notification is dropped.
func progressReporter(session *mcp.ServerSession, token any) phaseReporter {
	return func(ctx context.Context, phase acquirePhase) {
		_ = session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
			ProgressToken: token,
			Progress:      phase.step,
			Total:         acquirePhaseCount,
			Message:       phase.message,
		})
	}
}

// withProgress makes ctx carry a phase reporter when the request has a
// progress token and a server session to notify, and returns ctx
// unchanged otherwise.
func withProgress(ctx context.Context, req mcp.Request, params *mcp.CallToolParamsRaw) context.Context {
	token := params.GetProgressToken()
	session, ok := req.GetSession().(*mcp.ServerSession)
	if token == nil || !ok {
		return ctx
	}
	return withPhaseReporter(ctx, progressReporter(session, token))
}
