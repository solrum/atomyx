package process

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"dev.atomyx/internal/diag"
)

// defaultRunFallbackTimeout bounds a synchronous Run call when the
// caller's ctx carries no deadline of its own. It is a safety net
// against a wedged subprocess (a hung adb or xcrun invocation), not a
// tight budget: a legitimate device command, including installing a
// large app over a slow USB or network connection, is expected to
// finish well inside this bound, so it only ever fires on a genuine
// hang.
const defaultRunFallbackTimeout = 10 * time.Minute

// runFallbackTimeoutRemedy is the remedy text attached whenever Run's
// fallback timeout fires.
const runFallbackTimeoutRemedy = "Confirm the device and its command-line tooling are still responsive, then retry."

// Exec runs commands through the operating system's process loader.
// It is the production Runner: every adapter that shells out to a
// device-tooling CLI uses it outside of tests.
type Exec struct {
	fallbackTimeout time.Duration
}

// ExecOption configures an Exec constructed by NewExec.
type ExecOption func(*Exec)

// WithRunFallbackTimeout overrides defaultRunFallbackTimeout. It
// exists for a test that needs Run's fallback to fire in
// milliseconds instead of waiting out the production bound; the
// production default stays defaultRunFallbackTimeout unless a caller
// passes this option.
func WithRunFallbackTimeout(d time.Duration) ExecOption {
	return func(e *Exec) {
		e.fallbackTimeout = d
	}
}

// NewExec constructs an Exec runner.
func NewExec(opts ...ExecOption) *Exec {
	e := &Exec{fallbackTimeout: defaultRunFallbackTimeout}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

var _ Runner = (*Exec)(nil)

// Run implements Runner by invoking name with args via
// exec.CommandContext. A canceled or expired ctx kills the in-flight
// process and Run returns ctx.Err() rather than the raw "signal:
// killed" error the OS reports for the kill itself.
//
// When ctx carries no deadline of its own, Run applies fallbackTimeout
// as a safety net: if the command has not finished by then, Run kills
// it and returns a diag.CodeToolTimeout diagnostic. A caller-supplied
// deadline is never shortened; the fallback applies only in its
// absence.
func (e Exec) Run(ctx context.Context, name string, args ...string) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	runCtx := ctx
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, e.fallbackTimeout)
		defer cancel()
	}

	cmd := exec.CommandContext(runCtx, name, args...)
	if env := EnvFrom(ctx); len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	result := Result{Stdout: stdout.String(), Stderr: stderr.String()}
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	if runErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return result, ctxErr
		}
		if runCtx.Err() != nil {
			return result, diag.New(diag.CodeToolTimeout,
				fmt.Sprintf("%s did not finish within its fallback time budget", name),
				runFallbackTimeoutRemedy, runErr)
		}
		return result, runErr
	}
	return result, nil
}
