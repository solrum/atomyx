package runner

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/script"
)

// inlineSource is the location prefix of a script that has no source
// file.
const inlineSource = "inline"

// GateStep describes the step a StepGate is about to let run.
type GateStep struct {
	// Location is "<source path>:<line>" for a top-level step, or
	// "inline:<line>" when the runner has no source path. It is empty
	// for a nested step and for a top-level step with no known line.
	Location string
	// Depth is 0 for a top-level step and one higher for each level of
	// nesting.
	Depth int
	// StepIndex is the run-wide index the step is about to receive, the
	// same counter StepEvent.StepIndex reports.
	StepIndex int
	// Command is the step's command name.
	Command string
}

// StepGate is consulted before every step that is about to run. It may
// block, for example to hold the run at a breakpoint. A non-nil error
// ends the run as cancelled, and the runner returns that error from
// Run.
//
// run is valid only until BeforeStep returns. A gate must not keep it.
type StepGate interface {
	BeforeStep(ctx context.Context, step GateStep, run PausedRun) error
}

// PausedRun acts on a run that is blocked inside a StepGate. Both
// methods are safe to call from a goroutine other than the one running
// the script.
type PausedRun interface {
	// ExecuteStep runs one parsed step through the same dispatch the
	// run loop uses, in a child scope that reads the run's current
	// variables and captures and discards its writes. It touches no
	// run counter, emits no step event and records no step result. It
	// refuses a command that runs nested steps with an
	// INVALID_ARGUMENT error.
	ExecuteStep(ctx context.Context, step script.ScriptStep) (CommandResult, error)
	// Variables returns a copy of the run's current variables.
	Variables() VariableSnapshot
}

// VariableSnapshot is a copy of a run's variables taken at one moment.
// Mutating it never affects the run.
type VariableSnapshot struct {
	Values map[string]ScriptValue
	// Secret names the variables in Values that hold a secret.
	Secret map[string]bool
}

// ErrPausedRunClosed is returned by ExecuteStep once the gate call that
// supplied the PausedRun has returned.
var ErrPausedRunClosed = errors.New("the run is no longer paused")

// nestingCommand marks a command whose step runs other steps. Such a
// command cannot run in an isolated child scope.
type nestingCommand interface {
	runsNestedSteps()
}

func (RunFlowCommand) runsNestedSteps() {}
func (HandleCommand) runsNestedSteps()  {}
func (BranchCommand) runsNestedSteps()  {}

func snapshotVariables(variables map[string]ScriptValue, secrets map[string]bool) VariableSnapshot {
	values := make(map[string]ScriptValue, len(variables))
	for k, v := range variables {
		values[k] = v
	}
	secret := make(map[string]bool, len(secrets))
	for k, v := range secrets {
		secret[k] = v
	}
	return VariableSnapshot{Values: values, Secret: secret}
}

// stepLocation renders the location a gate sees for a top-level step.
func (r *ScriptRunner) stepLocation(line *int) string {
	if line == nil {
		return ""
	}
	source := r.deps.SourcePath
	if source == "" {
		source = inlineSource
	}
	return source + ":" + strconv.Itoa(*line)
}

// gateStep asks the configured gate whether step may run. A nil gate
// always allows it.
func (r *ScriptRunner) gateStep(ctx context.Context, rs *runState, location string, command string) error {
	if r.deps.StepGate == nil {
		return nil
	}
	run := &pausedRun{runner: r, rs: rs}
	defer run.close()
	return r.deps.StepGate.BeforeStep(ctx, GateStep{
		Location:  location,
		Depth:     rs.depth,
		StepIndex: rs.globalStepIndex,
		Command:   command,
	}, run)
}

// pausedRun is the PausedRun handed to a gate. The mutex orders the
// run's resumption after any in-flight read of the run's state.
type pausedRun struct {
	runner *ScriptRunner
	rs     *runState

	mu     sync.RWMutex
	closed bool
}

func (p *pausedRun) close() {
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
}

func (p *pausedRun) Variables() VariableSnapshot {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.closed {
		return VariableSnapshot{Values: map[string]ScriptValue{}, Secret: map[string]bool{}}
	}
	return snapshotVariables(p.rs.variables, p.rs.secrets)
}

func (p *pausedRun) ExecuteStep(ctx context.Context, step script.ScriptStep) (CommandResult, error) {
	child, err := p.childScope()
	if err != nil {
		return CommandResult{}, err
	}
	name := step.StepCommand()
	cmd, ok := p.runner.registry[name]
	if !ok {
		return CommandResult{}, diag.New(diag.CodeInvalidArgument,
			fmt.Sprintf("Unknown command: %q", name), diag.InvalidArgumentRemedy, nil)
	}
	if _, nests := cmd.(nestingCommand); nests {
		return CommandResult{}, diag.New(diag.CodeInvalidArgument,
			fmt.Sprintf("%q runs other steps and cannot run on its own", name),
			diag.InvalidArgumentRemedy, nil)
	}
	return p.runner.execute(cmd, step, p.runner.contextFor(ctx, child, 0)), nil
}

// childScope copies the run state a single step may read, so its
// writes never reach the paused run.
func (p *pausedRun) childScope() (*runState, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.closed {
		return nil, ErrPausedRunClosed
	}
	child := *p.rs
	scope := snapshotVariables(p.rs.variables, p.rs.secrets)
	child.variables = scope.Values
	child.secrets = scope.Secret
	child.captures = make(map[string]CapturedRequest, len(p.rs.captures))
	for k, v := range p.rs.captures {
		child.captures[k] = v
	}
	return &child, nil
}
