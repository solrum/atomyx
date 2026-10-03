package orchestra

import (
	"context"
	"time"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/logging"
)

// HealthCheckTimeoutMs bounds how long CheckHealth waits for the
// driver's liveness probe to answer before treating it as
// unreachable rather than waiting on the caller's own deadline.
const HealthCheckTimeoutMs = 5000

// HealthReport is the structured outcome of a liveness check, the
// shape both the CLI's doctor command and the MCP server's
// check_health tool render to their caller.
//
// Selected is false only when no device is bound at all, in which
// case Healthy, Code, Message, and Remedy carry their zero values.
// Selected true with Healthy false means a device is bound but its
// agent did not answer the liveness probe in time; Code and Remedy
// then describe the failure the same way any other coded error on
// this surface does.
//
// State names where the session stands: unselected, idle, running,
// released, borrowed or lost. A session in the released, borrowed or
// lost state is not probed, so Healthy is false there without meaning the device
// failed. The orchestra itself leaves State empty because it does not
// know the holder state.
type HealthReport struct {
	Selected bool   `json:"selected"`
	Healthy  bool   `json:"healthy"`
	State    string `json:"state,omitempty"`
	Code     string `json:"code,omitempty"`
	Message  string `json:"message,omitempty"`
	Remedy   string `json:"remedy,omitempty"`
}

// CheckHealth probes the bound driver's liveness, bounded by
// HealthCheckTimeoutMs. It always reports Selected true, since an
// Orchestra exists only once a device is connected; a caller with no
// device bound at all reports HealthReport{Selected: false} itself,
// before ever reaching an Orchestra.
//
// A failed probe, including one that misses its own deadline, is
// folded into an unhealthy report rather than returned as a Go
// error: an unreachable agent is a normal check outcome, not a
// failure of the check itself. CheckHealth has no error return at
// all, so a caller that wraps it in a tool or command surface should
// treat every call as successful and branch on Healthy, not on an
// error.
func (o *Orchestra) CheckHealth(ctx context.Context) HealthReport {
	pingCtx, cancel := context.WithTimeout(ctx, HealthCheckTimeoutMs*time.Millisecond)
	defer cancel()
	if err := o.driver.Ping(pingCtx); err != nil {
		code, message, remedy := diag.Describe(err)
		o.runLogger(ctx).Debug("orchestra.checkHealth", logging.LogContext{"healthy": false, "code": code})
		return HealthReport{Selected: true, Healthy: false, Code: code, Message: message, Remedy: remedy}
	}
	o.runLogger(ctx).Debug("orchestra.checkHealth", logging.LogContext{"healthy": true})
	return HealthReport{Selected: true, Healthy: true}
}
