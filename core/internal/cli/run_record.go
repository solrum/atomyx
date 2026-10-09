package cli

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/recording"
	"dev.atomyx/internal/report"
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/version"
)

// runRecord is the run folder of one CLI run. Every run and suite
// command opens one before it executes, whether or not a report was
// requested, and closes it on every path out of the command.
//
// The outcome starts as error, which is what a run reads as when the
// command returns before it records a verdict. The command narrows it
// through setOutcome once it knows how the run ended.
type runRecord struct {
	writer  *runfolder.Writer
	formats []string
	outcome runfolder.Outcome
	mutate  func(*runfolder.Meta)
	// reportedInJSON is true once the run folder path has been written
	// into the command's JSON output, which makes the plain path line
	// redundant.
	reportedInJSON bool
	// onEnd, when set, runs before the run folder is finalized, so a host
	// stops writing into it first.
	onEnd func()
	// capture is the screen recording of the run, nil until beginVideo.
	capture *recording.Capture
	// recording is the answer of the finished capture, nil when recording
	// was off or the video was discarded.
	recording *runfolder.Recording
}

// runIdentity describes what a run drives, for the meta of its run
// folder.
type runIdentity struct {
	Kind       runfolder.Kind
	Label      string
	ScriptPath string
	Device     device.Descriptor
	// ParentRunID is the sharding run that started this run as a worker,
	// empty for a run nothing started.
	ParentRunID string
}

// startRunRecord creates the run folder under out.Root. The folder id
// derives from identity.Label, and the meta records the device, the
// host version, and the script.
func startRunRecord(out runOutput, identity runIdentity) (*runRecord, error) {
	base := runfolder.Meta{
		Kind:        identity.Kind,
		ScriptPath:  identity.ScriptPath,
		ScriptName:  identity.Label,
		ParentRunID: identity.ParentRunID,
		DeviceID:    identity.Device.ID,
		Host:        &runfolder.Host{AtomyxVersion: version.Read().Version},
	}
	if identity.Device.ID != "" {
		base.Device = &runfolder.Device{
			Platform:   string(identity.Device.Platform),
			Kind:       string(identity.Device.Kind),
			HardwareID: identity.Device.ID,
			Name:       identity.Device.Name,
		}
	}
	writer, err := runfolder.NewStarter(out.Root, time.Now, rand.Reader).Start(identity.Label, base)
	if err != nil {
		return nil, fmt.Errorf("creating the run folder: %w", err)
	}
	return &runRecord{writer: writer, formats: out.Formats, outcome: runfolder.OutcomeError}, nil
}

// ID returns the run id, which also correlates the run's step trace.
func (r *runRecord) ID() string { return r.writer.ID() }

// Dir returns the run folder path.
func (r *runRecord) Dir() string { return r.writer.Dir() }

// reportOptions returns the formats and directories the report writer
// uses for this run.
func (r *runRecord) reportOptions() report.ReportOptions {
	return report.ReportOptions{
		Formats:      r.formats,
		ReportDir:    runfolder.ReportDir(r.writer.Dir()),
		ArtifactsDir: runfolder.ArtifactsDir(r.writer.Dir()),
	}
}

// setOutcome records how the run ended. mutate, when not nil, sets the
// end-of-run meta fields such as the step count.
func (r *runRecord) setOutcome(outcome runfolder.Outcome, mutate func(*runfolder.Meta)) {
	r.outcome, r.mutate = outcome, mutate
}

// outcomeForResult maps a finished run's pass state to an outcome.
func outcomeForResult(ok bool) runfolder.Outcome {
	if ok {
		return runfolder.OutcomePassed
	}
	return runfolder.OutcomeFailed
}

// outcomeForScript maps a finished script run to an outcome. A run that
// aborted because the device or agent went unreachable is deviceLost,
// not failed, since no step of the script is at fault.
func outcomeForScript(result runner.ScriptResult) runfolder.Outcome {
	if result.Abort != nil && result.Abort.Reason == runner.RunAbortDeviceLost {
		return runfolder.OutcomeDeviceLost
	}
	return outcomeForResult(result.Ok)
}

// end finalizes the run folder and announces its path on stderr. A
// stop signal that canceled ctx turns every outcome except passed into
// cancelled, because the runner reports a step interrupted by the
// signal as a failure the operator did not cause. When the meta cannot
// be written, the lock is still released and the failure is reported
// without changing the command's exit code.
func (r *runRecord) end(ctx context.Context, stderr io.Writer) {
	if r.onEnd != nil {
		r.onEnd()
	}
	r.settleOutcome(ctx)
	mutate := r.mutate
	if rec := r.finishVideo(ctx); rec != nil {
		mutate = func(m *runfolder.Meta) {
			if r.mutate != nil {
				r.mutate(m)
			}
			m.Recording = rec
		}
	}
	if err := r.writer.Finalize(r.outcome, mutate); err != nil {
		_, _ = fmt.Fprintf(stderr, "warning: could not finalize the run folder: %s\n", err)
		_ = r.writer.Close()
	}
	if !r.reportedInJSON {
		_, _ = fmt.Fprintf(stderr, "Run folder: %s\n", r.writer.Dir())
	}
}

// scriptMeta returns the mutate function that records a script result's
// step counts.
func scriptMeta(totalSteps int, failedAtStep *int) func(*runfolder.Meta) {
	return func(m *runfolder.Meta) {
		m.TotalSteps = totalSteps
		m.FailedAtStep = failedAtStep
	}
}

// runLabel picks the label of a run folder: the declared name of the
// script or scenario, or the base name of its file without extension
// when it declares none.
func runLabel(declaredName, path string) string {
	if declaredName != "" {
		return declaredName
	}
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// printReportLocation tells the operator where the rendered report
// is. It prints nothing when the run requested no report format.
func printReportLocation(stderr io.Writer, record *runRecord, bundle report.ReportBundle) {
	if len(record.formats) == 0 {
		return
	}
	_, _ = fmt.Fprintf(stderr, "Report: %s\n", bundle.BundleDir)
}
