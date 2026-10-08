package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"dev.atomyx/internal/debugrun"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/scenario"
	"dev.atomyx/internal/script"
	"dev.atomyx/internal/session"
)

// usageError marks a problem with how the command was invoked, which
// the run command reports with its usage exit code.
type usageError struct{ message string }

func (e usageError) Error() string { return e.message }

// runDebug is the debugging side of one run command: the adapter that
// holds the run at its breakpoints, and the terminal that shows where
// the run stopped and resumes it on Enter.
type runDebug struct {
	adapter *debugrun.Adapter
	prompt  io.Writer
	stderr  io.Writer
	stdin   io.Reader

	sess     *session.Session
	pauser   *promptPauser
	stopKeys func()
}

// newRunDebug returns nil when the command has no --break flag. It
// refuses a terminal-less stdin and a location that matches no
// authored step of the scripts, both before any device is taken.
func newRunDebug(deps Deps, flags map[string]FlagValue, loadScripts func() ([]runner.BreakScript, error), jsonOutput bool) (*runDebug, error) {
	specs := flagStrings(flags, "--break")
	if len(specs) == 0 {
		return nil, nil
	}
	if !deps.StdinTerminal {
		return nil, usageError{"--break needs a terminal on standard input, because a paused run waits for a key. Run without --break, or run from a terminal."}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	locations := make([]runner.BreakLocation, 0, len(specs))
	for _, spec := range specs {
		loc, err := runner.ParseBreakLocation(spec, cwd)
		if err != nil {
			return nil, usageError{fmt.Sprintf("--break: %s", describeMessage(err))}
		}
		locations = append(locations, loc)
	}
	scripts, err := loadScripts()
	if err != nil {
		return nil, err
	}
	if err := runner.ValidateBreakLocations(locations, scripts); err != nil {
		return nil, usageError{fmt.Sprintf("--break: %s", describeMessage(err))}
	}
	prompt := deps.stdout()
	if jsonOutput {
		prompt = deps.stderr()
	}
	return &runDebug{
		adapter: debugrun.New(debugrun.Options{
			BaseDir:     cwd,
			Breakpoints: locations,
			Scripts:     func() []runner.BreakScript { return scripts },
		}),
		prompt: prompt,
		stderr: deps.stderr(),
		stdin:  deps.Stdin,
	}, nil
}

func describeMessage(err error) string {
	_, message, _ := diag.Describe(err)
	return message
}

// debugger returns the session port to inject, or nil when the command
// is not debugging.
func (d *runDebug) debugger() session.Debugger {
	if d == nil {
		return nil
	}
	return d.adapter
}

// gate returns the runner gate, or nil when the command is not
// debugging.
func (d *runDebug) gate() runner.StepGate {
	if d == nil {
		return nil
	}
	return d.adapter
}

// attach binds the adapter to the opened session and starts reading
// Enter. stop must be called when the run ends.
func (d *runDebug) attach(ctx context.Context, opened *session.Session, orch *orchestra.Orchestra) {
	if d == nil {
		return
	}
	d.sess = opened
	d.pauser = &promptPauser{Session: opened, out: d.prompt}
	d.adapter.SetFinder(orch)
	d.adapter.Bind(d.pauser)
	enter, stopReading := readEnterKeys(d.stdin)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-enter:
				d.resumeOnEnter(ctx)
			case <-done:
				return
			}
		}
	}()
	d.stopKeys = func() {
		close(done)
		stopReading()
	}
}

// stop ends the Enter reader.
func (d *runDebug) stop() {
	if d == nil {
		return
	}
	if d.stopKeys != nil {
		d.stopKeys()
	}
}

// reportUnhit warns about each breakpoint the finished run never reached.
func (d *runDebug) reportUnhit() {
	if d == nil {
		return
	}
	for _, loc := range d.adapter.Unhit() {
		_, _ = fmt.Fprintf(d.stderr, "warning: the breakpoint at %s was never reached.\n", loc)
	}
}

// resumeOnEnter resumes a paused run. When the advisory check says the
// next step cannot find its target, the first Enter explains and the
// next one resumes anyway.
func (d *runDebug) resumeOnEnter(ctx context.Context) {
	if _, paused := d.sess.Paused(); !paused {
		return
	}
	res, err := d.sess.Resume(ctx, session.ResumeOptions{Force: d.pauser.takeForce()})
	if err != nil || res.Resumed {
		return
	}
	d.pauser.armForce()
	_, _ = fmt.Fprintf(d.prompt, "%s. Press Enter again to resume anyway.\n", res.MissReason)
}

// promptPauser shows where the run stopped on the terminal, in addition
// to marking the session paused.
type promptPauser struct {
	*session.Session
	out io.Writer

	mu    sync.Mutex
	force bool
}

func (p *promptPauser) EnterPause(loc session.PauseLocation) {
	p.Session.EnterPause(loc)
	p.mu.Lock()
	p.force = false
	p.mu.Unlock()
	_, _ = fmt.Fprintf(p.out, "Paused %s. Press Enter to resume.\n", describePause(loc))
}

func (p *promptPauser) takeForce() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	force := p.force
	p.force = false
	return force
}

func (p *promptPauser) armForce() {
	p.mu.Lock()
	p.force = true
	p.mu.Unlock()
}

func describePause(loc session.PauseLocation) string {
	if loc.Line > 0 {
		return fmt.Sprintf("at %s:%d, before %s", loc.Source, loc.Line, loc.Command)
	}
	return fmt.Sprintf("in %s, before the nested %s step", loc.Source, loc.Command)
}

// breakScriptsOf parses the file a run executes, so break locations can
// be checked against its authored steps.
func breakScriptsOf(absPath, content string, externalEnv map[string]string) ([]runner.BreakScript, error) {
	if script.IsScenarioYaml(content) {
		scenarioDef, err := script.ParseScenario(content)
		if err != nil {
			return nil, err
		}
		dir := filepath.Dir(absPath)
		return scenario.BreakScripts(scenarioDef, externalEnv, fsScenarioScriptLoader(dir), dir)
	}
	def, err := script.ParseScript(content, externalEnv)
	if err != nil {
		return nil, err
	}
	return []runner.BreakScript{{Path: absPath, Definition: def}}, nil
}

// noteFile tells the adapter about a screenshot file the run folder just
// received, so an act that took the screenshot can name it.
func (d *runDebug) noteFile(file string) {
	if d != nil {
		d.adapter.NoteArtifact(file)
	}
}
