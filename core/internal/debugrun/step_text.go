package debugrun

import (
	"strings"

	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/script"
)

// writtenText renders step as the person wrote it, for the run folder.
// The text of a type step is always masked, because anything typed may be
// a password. Secret values elsewhere are redacted. A step that cannot be
// rebuilt as YAML is recorded by its command name alone, so the record
// never shows more than the rebuild proved safe.
func writtenText(step script.ScriptStep, redactor *runner.Redactor) string {
	shown := step
	if typed, ok := step.(*script.TypeStep); ok {
		masked := *typed
		masked.Text = orchestra.SecureMask
		shown = &masked
	}
	text, err := script.RenderStepYAML(shown)
	if err != nil {
		return step.StepCommand()
	}
	return redactor.String(strings.TrimSpace(text))
}

// NoteArtifact records a run-relative file the step in flight produced,
// such as the screenshot of an act. The host that writes artifacts calls
// it, and a file noted while no act runs is ignored.
func (a *Adapter) NoteArtifact(file string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.actRunning {
		a.actFiles = append(a.actFiles, file)
	}
}

// collectArtifacts starts collecting noted files. The returned function
// stops collecting and returns them.
func (a *Adapter) collectArtifacts() (finish func() []string) {
	a.mu.Lock()
	a.actRunning, a.actFiles = true, nil
	a.mu.Unlock()
	return func() []string {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.actRunning = false
		return a.actFiles
	}
}
