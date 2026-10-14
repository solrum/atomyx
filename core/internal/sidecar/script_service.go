package sidecar

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/scenario"
	"dev.atomyx/internal/script"
)

// errRunAlreadyInProgress is returned by Run when a script is already
// executing against this session.
var errRunAlreadyInProgress = errors.New("A run is already in progress") //nolint:staticcheck // this exact string crosses the wire protocol and is matched verbatim by other sidecar implementations, so its casing is part of the contract, not a style choice

// activeRun tracks the single in-flight run this service allows at a
// time: its id, for correlating events, and its cancel func, so Stop
// can end it cooperatively.
type activeRun struct {
	runID  string
	cancel context.CancelFunc
}

// scriptService executes a single script's YAML, or a scenario's
// ordered group of scripts, against the current session's device and
// publishes lifecycle events on the EventBus so a host UI can render
// progress live: runStarted, stepStarted, stepCompleted, runCompleted,
// runErrored, and runStopRequested for a single script; the same
// runStarted/runCompleted/runErrored/runStopRequested set plus
// scenarioStarted, scriptStarted, scriptCompleted, and
// scenarioCompleted for a scenario.
type scriptService struct {
	session *Session
	events  *EventBus
	logger  logging.Logger

	mu      sync.Mutex
	current *activeRun

	parseScript   func(yamlContent string, externalEnv map[string]string) (*script.ScriptDefinition, error)
	parseScenario func(yamlContent string) (*script.ScenarioDefinition, error)
}

func newScriptService(session *Session, events *EventBus, logger logging.Logger) *scriptService {
	return &scriptService{
		session: session,
		events:  events,
		logger:  logger,
		parseScript: func(yamlContent string, externalEnv map[string]string) (*script.ScriptDefinition, error) {
			return script.ParseScript(yamlContent, externalEnv)
		},
		parseScenario: script.ParseScenario,
	}
}

// reserve atomically claims the single run slot this session allows,
// installing a placeholder activeRun so a concurrent Run or
// RunScenario call sees the slot occupied and is rejected before it
// starts parsing. Run and RunScenario upgrade the placeholder to a
// real activeRun once parsing succeeds, or release it via
// clearCurrent on parse failure. Reserving before parsing closes the
// window a caller could otherwise race through between checking
// s.current and setting it.
func (s *scriptService) reserve() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current != nil {
		return errRunAlreadyInProgress
	}
	s.current = &activeRun{}
	return nil
}

func (s *scriptService) Run(ctx context.Context, yaml string) (runScriptResultWire, error) {
	selection, err := requireDeviceOrWireError(s.session)
	if err != nil {
		return runScriptResultWire{}, err
	}

	if reserveErr := s.reserve(); reserveErr != nil {
		return runScriptResultWire{}, reserveErr
	}

	// The run slot is reserved above, before parsing, so a caller
	// polling isRunning against a script that fails to parse observes
	// the slot occupied only for the duration of the parse itself: on
	// parse failure clearCurrent releases it immediately, and on
	// success Run upgrades the placeholder to the real activeRun
	// below. Reserving first (rather than parsing first and reserving
	// after) is what stops a second concurrent Run call from also
	// passing the guard and starting its own parse.
	def, err := s.parseScript(yaml, nil)
	if err != nil {
		s.clearCurrent()
		return runScriptResultWire{}, err
	}

	runID := newRunID()
	runCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.current = &activeRun{runID: runID, cancel: cancel}
	s.mu.Unlock()
	s.session.SetActiveRunID(&runID)
	defer cancel()

	secrets := runner.NewRunSecrets()
	sr := runner.NewScriptRunner(runner.ScriptRunnerDeps{
		Orchestra:  selection.Orchestra,
		Clock:      orchestra.SystemClock{},
		Logger:     logging.Masked(s.logger, secrets.String),
		Commands:   runner.DefaultCommands(),
		SecretSink: secrets,
		OnStep: func(event runner.StepEvent) {
			s.events.Emit(Event{Event: string(event.Type), Payload: toStepEventWire(runID, event.Masked(secrets.String))})
		},
	})

	s.events.Emit(Event{
		Event: "runStarted",
		Payload: runStartedPayload{
			RunID:      runID,
			StartedAt:  time.Now().UnixMilli(),
			ScriptName: def.Name,
		},
	})

	result, runErr := sr.Run(runCtx, def)
	s.clearCurrent()
	result = result.Masked(secrets.String)

	if runErr != nil {
		s.events.Emit(Event{
			Event:   "runErrored",
			Payload: runErroredPayload{RunID: runID, Message: secrets.String(runErr.Error())},
		})
		return runScriptResultWire{}, runErr
	}

	s.events.Emit(Event{
		Event: "runCompleted",
		Payload: runCompletedPayload{
			RunID:      runID,
			Ok:         result.Ok,
			Passed:     result.PassedSteps,
			Total:      result.TotalSteps,
			DurationMs: result.DurationMs,
		},
	})
	return runScriptResultWire{RunID: runID, Result: toScriptResultWire(result)}, nil
}

// RunScenario parses yaml as a scenario definition and runs its child
// scripts in order against the current session's device, publishing
// the same runStarted/runCompleted/runErrored lifecycle events Run
// does, plus every scenario-level event (scenarioStarted,
// scriptStarted, scriptCompleted, scenarioCompleted) and per-step
// event the scenario runner emits, all correlated by runId. Each
// child script's path is resolved against cwd before loading, exactly
// as named in the scenario's scripts list.
func (s *scriptService) RunScenario(ctx context.Context, yaml string, cwd string) (runScenarioResultWire, error) {
	selection, err := requireDeviceOrWireError(s.session)
	if err != nil {
		return runScenarioResultWire{}, err
	}

	if reserveErr := s.reserve(); reserveErr != nil {
		return runScenarioResultWire{}, reserveErr
	}

	def, err := s.parseScenario(yaml)
	if err != nil {
		s.clearCurrent()
		return runScenarioResultWire{}, err
	}

	runID := newRunID()
	runCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.current = &activeRun{runID: runID, cancel: cancel}
	s.mu.Unlock()
	s.session.SetActiveRunID(&runID)
	defer cancel()

	secrets := runner.NewRunSecrets()
	sr := scenario.NewScenarioRunner(scenario.ScenarioRunnerDeps{
		Orchestra:  selection.Orchestra,
		Clock:      orchestra.SystemClock{},
		Logger:     logging.Masked(s.logger, secrets.String),
		Commands:   runner.DefaultCommands(),
		SecretSink: secrets,
		LoadScript: func(relPath string) (string, error) {
			data, err := os.ReadFile(resolveScriptPath(cwd, relPath))
			if err != nil {
				return "", err
			}
			return string(data), nil
		},
		OnStep: func(event runner.StepEvent) {
			s.events.Emit(Event{Event: string(event.Type), Payload: toStepEventWire(runID, event.Masked(secrets.String))})
		},
		OnScenarioEvent: func(event scenario.ScenarioEvent) {
			s.events.Emit(Event{Event: string(event.Type), Payload: toScenarioEventWire(runID, event)})
		},
	})

	s.events.Emit(Event{
		Event: "runStarted",
		Payload: runStartedScenarioPayload{
			RunID:        runID,
			StartedAt:    time.Now().UnixMilli(),
			ScenarioName: def.Name,
			TotalScripts: len(def.Scripts),
		},
	})

	result, runErr := sr.Run(runCtx, def)
	s.clearCurrent()
	result = result.Masked(secrets.String)

	if runErr != nil {
		s.events.Emit(Event{
			Event:   "runErrored",
			Payload: runErroredPayload{RunID: runID, Message: secrets.String(runErr.Error())},
		})
		return runScenarioResultWire{}, runErr
	}

	s.events.Emit(Event{
		Event: "runCompleted",
		Payload: runCompletedScenarioPayload{
			RunID:         runID,
			Ok:            result.Ok,
			PassedScripts: result.PassedScripts,
			TotalScripts:  result.TotalScripts,
			DurationMs:    result.DurationMs,
		},
	})
	return runScenarioResultWire{RunID: runID, Result: toScenarioResultWire(result)}, nil
}

// resolveScriptPath resolves a scenario child script's path against
// cwd, matching Node's path.resolve(cwd, relPath): an absolute
// relPath is used as-is (cleaned), a relative one is joined onto cwd.
func resolveScriptPath(cwd, relPath string) string {
	if filepath.IsAbs(relPath) {
		return filepath.Clean(relPath)
	}
	return filepath.Join(cwd, relPath)
}

// Stop cancels the in-flight run, if any, and publishes
// runStopRequested. It is a no-op when no run is in flight, and also
// when the run slot is reserved but still parsing: the reservation
// placeholder carries no cancel func yet, since the context it would
// cancel is not created until parsing succeeds.
//
// runStopRequested is emitted before current.cancel() runs, not
// after: Run's goroutine only observes the cancellation once the
// context's done channel closes inside cancel(), so publishing the
// event first guarantees runStopRequested always precedes the
// runErrored that cancellation goes on to produce, regardless of how
// the two goroutines happen to get scheduled.
func (s *scriptService) Stop() {
	s.mu.Lock()
	current := s.current
	s.mu.Unlock()
	if current == nil || current.cancel == nil {
		return
	}
	s.events.Emit(Event{Event: "runStopRequested", Payload: runStopRequestedPayload{RunID: current.runID}})
	current.cancel()
}

// IsRunning reports whether a run is currently in flight.
func (s *scriptService) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current != nil
}

func (s *scriptService) clearCurrent() {
	s.mu.Lock()
	s.current = nil
	s.mu.Unlock()
	s.session.SetActiveRunID(nil)
}
