package conformance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"dev.atomyx/internal/device"
)

// eventedCaller is the Caller plus the ordered event log every
// differential replay in this file needs: goSidecarCaller implements
// it.
type eventedCaller interface {
	Caller
	Events() []Event
}

// sidecarStepOutcome is one step's live outcome: exactly one of
// Result or ErrorCode is set. Only the wire error CODE is compared,
// not the message, because a runScript parse failure surfaces the
// underlying script parser's error text verbatim, and that text is
// not expected to match word for word between the Go and zod
// validators (the script package's own differential test excludes it
// for the same reason). Every other field here is part of the wire
// contract this test holds to byte-exact parity.
type sidecarStepOutcome struct {
	Method    string          `json:"method"`
	Result    json.RawMessage `json:"result,omitempty"`
	ErrorCode string          `json:"errorCode,omitempty"`
}

type sidecarTraceEvent struct {
	Event   string          `json:"event"`
	Payload json.RawMessage `json:"payload"`
}

// sidecarTrace is the comparable shape a whole case replay produces:
// every step's outcome, in order, followed by every event the engine
// published, in publish order. Diffing this struct catches a response
// mismatch and an event-stream mismatch (a missing, extra, or
// reordered event) in one comparison.
type sidecarTrace struct {
	Steps  []sidecarStepOutcome `json:"steps"`
	Events []sidecarTraceEvent  `json:"events"`
}

// sidecarNondeterministicFields names the wire fields whose value can
// never match a frozen golden byte-for-byte: a fresh random run id
// (a crypto/rand UUID minted per run) and any wall-clock timestamp or
// duration, which depends on real elapsed time the ScriptRunner's own
// clock is not insulated from. The golden still carries these fields
// at their normal wire position; normalizeSidecarValue replaces the
// value so presence and every other field remain part of the
// comparison.
var sidecarNondeterministicFields = map[string]bool{
	"runId":      true,
	"startedAt":  true,
	"durationMs": true,
	// runErrored's payload carries the underlying cancellation error's
	// own message, Go's ctx.Err() rendering as "context canceled".
	// This is not a sidecar-authored wire string, so, like the script
	// package's own differential excluding the parser's own error
	// text, only the event's occurrence and its runId are compared.
	"message": true,
}

const sidecarNormalizedPlaceholder = "<normalized>"

func normalizeSidecarJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return raw
	}
	out, err := json.Marshal(normalizeSidecarValue(value))
	if err != nil {
		return raw
	}
	return out
}

func normalizeSidecarValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, vv := range val {
			if sidecarNondeterministicFields[k] {
				out[k] = sidecarNormalizedPlaceholder
				continue
			}
			out[k] = normalizeSidecarValue(vv)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, vv := range val {
			out[i] = normalizeSidecarValue(vv)
		}
		return out
	default:
		return val
	}
}

// callOutcome runs one step against caller and classifies the
// result: a wire-level error becomes an ErrorCode, a transport
// failure (the process died, a response could not be decoded) is
// returned as err for the caller to fail the test outright rather
// than record it as a comparable outcome.
func callOutcome(ctx context.Context, caller Caller, method string, params any) (sidecarStepOutcome, error) {
	result, err := caller.Call(ctx, method, params)
	if err == nil {
		return sidecarStepOutcome{Method: method, Result: normalizeSidecarJSON(result)}, nil
	}
	var rpcErr *RPCError
	if errors.As(err, &rpcErr) {
		return sidecarStepOutcome{Method: method, ErrorCode: rpcErr.Code}, nil
	}
	return sidecarStepOutcome{}, fmt.Errorf("calling %s: %w", method, err)
}

// replaySidecarSteps sends every step to caller in order, awaiting
// each response before sending the next, and returns the resulting
// trace. onDeviceSelected, when non-nil, runs immediately after a
// selectDevice step that did not error. The Go caller uses it to
// stage the case's driver program onto the freshly selected
// mock.Driver, mirroring what the TypeScript entrypoint's factory
// already did by the time it answered the same selectDevice call.
func replaySidecarSteps(ctx context.Context, caller eventedCaller, steps []SidecarStep, onDeviceSelected func() error) (sidecarTrace, error) {
	var trace sidecarTrace
	for _, step := range steps {
		var params any
		if len(step.Params) > 0 {
			params = step.Params
		}
		outcome, err := callOutcome(ctx, caller, step.Method, params)
		if err != nil {
			return sidecarTrace{}, err
		}
		trace.Steps = append(trace.Steps, outcome)
		if step.Method == "selectDevice" && outcome.ErrorCode == "" && onDeviceSelected != nil {
			if err := onDeviceSelected(); err != nil {
				return sidecarTrace{}, fmt.Errorf("staging driver program: %w", err)
			}
		}
	}
	for _, e := range caller.Events() {
		trace.Events = append(trace.Events, sidecarTraceEvent{Event: e.Name, Payload: normalizeSidecarJSON(e.Payload)})
	}
	return trace, nil
}

// runSidecarConcurrentStop drives the one non-linear corpus shape: it
// selects the device, fires runScript in the background, waits
// spec.StopAfterMs of real time (comfortably inside the script's own
// spec.StepDelayMs inter-step delay, see corpus/sidecar/format.md for
// the margin), then calls stopScript and isRunning while the run is
// still in flight, joins the backgrounded runScript call, and calls
// isRunning once more. This is the only device-free path that
// produces a genuine runErrored event instead of a graceful
// failed-step runCompleted: every command failure is caught per-step
// and reported as an ok:false step, so only a cancellation observed
// between steps (never inside one) aborts the run itself.
func runSidecarConcurrentStop(ctx context.Context, caller eventedCaller, snapshot []device.Descriptor, spec *SidecarConcurrentStop, onDeviceSelected func() error) (sidecarTrace, error) {
	var trace sidecarTrace

	deviceID := ""
	if len(snapshot) > 0 {
		deviceID = snapshot[0].ID
	}
	selectOutcome, err := callOutcome(ctx, caller, "selectDevice", map[string]any{"id": deviceID})
	if err != nil {
		return sidecarTrace{}, err
	}
	trace.Steps = append(trace.Steps, selectOutcome)
	if selectOutcome.ErrorCode != "" {
		return sidecarTrace{}, fmt.Errorf("selectDevice failed unexpectedly: %s", selectOutcome.ErrorCode)
	}
	if onDeviceSelected != nil {
		if err := onDeviceSelected(); err != nil {
			return sidecarTrace{}, fmt.Errorf("staging driver program: %w", err)
		}
	}

	type runOutcome struct {
		outcome sidecarStepOutcome
		err     error
	}
	done := make(chan runOutcome, 1)
	go func() {
		outcome, err := callOutcome(ctx, caller, "runScript", map[string]any{"yaml": spec.Yaml})
		done <- runOutcome{outcome, err}
	}()

	select {
	case <-time.After(time.Duration(spec.StopAfterMs) * time.Millisecond):
	case <-ctx.Done():
		return sidecarTrace{}, ctx.Err()
	}

	isRunningDuring, err := callOutcome(ctx, caller, "isRunning", nil)
	if err != nil {
		return sidecarTrace{}, err
	}
	trace.Steps = append(trace.Steps, isRunningDuring)

	stopOutcome, err := callOutcome(ctx, caller, "stopScript", nil)
	if err != nil {
		return sidecarTrace{}, err
	}
	trace.Steps = append(trace.Steps, stopOutcome)

	select {
	case run := <-done:
		if run.err != nil {
			return sidecarTrace{}, run.err
		}
		trace.Steps = append(trace.Steps, run.outcome)
	case <-ctx.Done():
		return sidecarTrace{}, fmt.Errorf("runScript did not return after stopScript: %w", ctx.Err())
	}

	isRunningAfter, err := callOutcome(ctx, caller, "isRunning", nil)
	if err != nil {
		return sidecarTrace{}, err
	}
	trace.Steps = append(trace.Steps, isRunningAfter)

	// stepStarted/stepCompleted events are excluded here, unlike every
	// other case's replay: exactly how many steps complete before
	// stopScript's cancellation is observed depends on real-time
	// scheduling (stopAfterMs races the runner's own inter-step delay),
	// so that count is not guaranteed reproducible against a frozen
	// golden. The run's LIFECYCLE point is guaranteed regardless of
	// that race (runStarted, runStopRequested, runErrored, in that
	// order, with isRunning true then false around them), so those
	// events are still compared in full.
	for _, e := range caller.Events() {
		if e.Name == "stepStarted" || e.Name == "stepCompleted" {
			continue
		}
		trace.Events = append(trace.Events, sidecarTraceEvent{Event: e.Name, Payload: normalizeSidecarJSON(e.Payload)})
	}
	return trace, nil
}

// runGoSidecarCase replays c against a fresh in-process Go sidecar and
// returns the resulting trace, for the caller to turn into a golden
// comparable.
func runGoSidecarCase(t *testing.T, c *SidecarCase) sidecarTrace {
	t.Helper()

	goCaller, closeGo := newGoSidecarCaller(c.DeviceSnapshots)
	defer closeGo()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	goStage := func() error { return stageMockDriver(goCaller.session, c.Driver) }

	if c.ConcurrentStop != nil {
		snapshot := []device.Descriptor{}
		if len(c.DeviceSnapshots) > 0 {
			snapshot = c.DeviceSnapshots[0]
		}
		goTrace, err := runSidecarConcurrentStop(ctx, goCaller, snapshot, c.ConcurrentStop, goStage)
		if err != nil {
			t.Fatalf("Go concurrent-stop replay: %v", err)
		}
		return goTrace
	}

	goTrace, err := replaySidecarSteps(ctx, goCaller, c.Steps, goStage)
	if err != nil {
		t.Fatalf("Go replay: %v", err)
	}
	return goTrace
}

func TestSidecarDifferential(t *testing.T) {
	cases, err := LoadSidecarCorpus("corpus/sidecar")
	if err != nil {
		t.Fatalf("loading sidecar corpus: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("sidecar corpus is empty")
	}

	for _, c := range cases {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			goTrace := runGoSidecarCase(t, c)

			goJSON, err := json.Marshal(goTrace)
			if err != nil {
				t.Fatalf("marshaling Go trace: %v", err)
			}

			checkGolden(t, filepath.Join("sidecar", c.Name+".json"), goJSON, DiffJSON)
		})
	}
}
