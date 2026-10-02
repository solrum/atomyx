package androiddriver_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dev.atomyx/internal/androiddriver"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/process"
	"dev.atomyx/internal/wire"
)

// TestAndroidDriver_BusyAgentResponse_BecomesARetryableNotReady proves the
// 429 the agent returns for a request that stayed queued behind another
// action reaches the caller as AGENT_NOT_READY with a remedy that says the
// call did not run, instead of as a generic rejection.
func TestAndroidDriver_BusyAgentResponse_BecomesARetryableNotReady(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/actions/tap_coords" {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"The agent is still running another action. Retry the call shortly.","retryable":true}`))
			return
		}
		_, _ = w.Write([]byte(faultInjectionOkBody("/ping")))
	}))
	t.Cleanup(server.Close)
	d := androiddriver.New(newFaultInjectionClient(t, server), process.NewFake(), androiddriver.Options{Serial: "S1"})
	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	err := d.Tap(context.Background(), wire.Point{X: 1, Y: 1})

	code, message, remedy := diag.Describe(err)
	if code != diag.CodeAgentNotReady {
		t.Fatalf("Tap() code = %q, want %q (error: %v)", code, diag.CodeAgentNotReady, err)
	}
	if !strings.Contains(message, "still running another action") || !strings.Contains(remedy, "did not run") {
		t.Fatalf("message = %q, remedy = %q, want the busy agent named and the retry remedy", message, remedy)
	}
}
