package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/recording"
	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/scenario"
)

type dirArtifacts struct{ dir string }

func (a dirArtifacts) ArtifactsDir() (string, error) { return a.dir, nil }

func (a dirArtifacts) AdoptArtifact(tmp, name string) (string, error) {
	dst := filepath.Join(a.dir, name)
	return dst, os.Rename(tmp, dst)
}

func deviceLostScript() *scenario.SuiteEntryOutcome {
	return &scenario.SuiteEntryOutcome{Script: &runner.ScriptResult{
		Abort: &runner.RunAbort{Reason: runner.RunAbortDeviceLost},
	}}
}

func deviceLostScenario() *scenario.SuiteEntryOutcome {
	return &scenario.SuiteEntryOutcome{Scenario: &scenario.ScenarioResult{Scripts: []scenario.ScenarioScriptResult{
		{Status: scenario.StatusPassed, Result: &runner.ScriptResult{Ok: true}},
		{Status: scenario.StatusFailed, Result: &runner.ScriptResult{Abort: &runner.RunAbort{Reason: runner.RunAbortDeviceLost}}},
	}}}
}

func TestSuiteVideoDiscardsAnEntryThatEndedWithADeviceLoss(t *testing.T) {
	cases := map[string]struct {
		outcome  *scenario.SuiteEntryOutcome
		wantKept bool
	}{
		"script device loss":           {outcome: deviceLostScript()},
		"scenario device loss":         {outcome: deviceLostScenario()},
		"script abort other than loss": {outcome: &scenario.SuiteEntryOutcome{Script: &runner.ScriptResult{Abort: &runner.RunAbort{Reason: runner.RunAbortProxyRequired}}}, wantKept: true},
		"plain failure":                {outcome: &scenario.SuiteEntryOutcome{Script: &runner.ScriptResult{}}, wantKept: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rec := &fakeVideoRecorder{segments: []string{".tmp-a.mp4"}}
			videos := newSuiteVideos(recording.NewLifecycle(rec, logging.NewStructuredLogger(io.Discard, logging.LevelWarn)), device.Descriptor{}, dirArtifacts{dir: t.TempDir()})
			end := videos.start(context.Background(), scenario.EntryTarget{EntryIndex: 0, Attempt: 1})

			kept := end(scenario.SuiteEntryResult{Status: scenario.EntryFailed, Result: tc.outcome})

			if tc.wantKept {
				if rec.stops != 1 || len(kept) != 1 {
					t.Errorf("stops=%d kept=%v, want the video kept", rec.stops, kept)
				}
				return
			}
			if rec.stops != 0 || rec.discards != 1 || kept != nil {
				t.Errorf("stops=%d discards=%d kept=%v, want a discard without a stop", rec.stops, rec.discards, kept)
			}
		})
	}
}
