package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/shutdown"
	"dev.atomyx/internal/storageroot"
)

func writeScript(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func mockDeps(stdout, stderr *bytes.Buffer) Deps {
	clock := orchestra.NewFakeClock(0)
	return Deps{
		Probe:   device.NewStaticProbe(nil),
		Factory: device.NewMockDriverFactoryWithDeps(device.MockDriverFactoryDeps{Clock: clock}),
		Clock:   clock,
		Stdout:  stdout,
		Stderr:  stderr,
	}
}

func readMeta(t *testing.T, runDir string) runfolder.Meta {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(runDir, runfolder.MetaFileName))
	if err != nil {
		t.Fatalf("reading meta: %v", err)
	}
	var meta runfolder.Meta
	if err := json.Unmarshal(body, &meta); err != nil {
		t.Fatalf("decoding meta: %v", err)
	}
	return meta
}

func TestRunScriptWritesFinalizedRunFolderWithLinkedArtifacts(t *testing.T) {
	root := t.TempDir()
	scriptPath := writeScript(t, "login.yml", "name: Login flow\n---\n- screenshot: home\n")

	var stdout, stderr bytes.Buffer
	code := RunScript(context.Background(), mockDeps(&stdout, &stderr), map[string]FlagValue{
		"--file": scriptPath, "--report-dir": root,
	})
	if code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr.String())
	}

	runDir := singleRunDir(t, root)
	meta := readMeta(t, runDir)
	if meta.Kind != runfolder.KindScript || meta.Result != runfolder.ResultPassed || meta.Outcome != runfolder.OutcomePassed {
		t.Errorf("meta = kind %q result %q outcome %q, want script passed passed", meta.Kind, meta.Result, meta.Outcome)
	}
	if meta.CompletedAt == nil || meta.ScriptName != "Login flow" || meta.ScriptPath != scriptPath || meta.TotalSteps != 1 {
		t.Errorf("meta is missing run details: %+v", meta)
	}
	if meta.Device == nil || meta.Host == nil || meta.DeviceID == "" {
		t.Errorf("meta is missing device or host: %+v", meta)
	}
	if !strings.Contains(filepath.Base(runDir), "-login-flow-") {
		t.Errorf("run folder %q does not carry the script name label", runDir)
	}

	shots, err := os.ReadDir(runfolder.ArtifactsDir(runDir))
	if err != nil || len(shots) != 1 {
		t.Fatalf("artifacts = %v, err %v, want one screenshot", shots, err)
	}
	html := string(findReportHTML(t, root))
	if !strings.Contains(html, `<img src="../artifacts/`+shots[0].Name()+`"`) {
		t.Errorf("report.html does not link ../artifacts/%s:\n%s", shots[0].Name(), html)
	}
	if !strings.Contains(stderr.String(), "Run folder: "+runDir+"\n") {
		t.Errorf("stderr does not announce the run folder %q: %q", runDir, stderr.String())
	}
}

func TestRunScriptWithoutReportStillWritesRunFolderUnderStorageRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv(storageroot.EnvVar, root)
	scriptPath := writeScript(t, "flow.yml", "- screenshot: home\n")

	var stdout, stderr bytes.Buffer
	code := RunScript(context.Background(), mockDeps(&stdout, &stderr), map[string]FlagValue{"--file": scriptPath})
	if code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr.String())
	}

	runDir := singleRunDir(t, root)
	if meta := readMeta(t, runDir); meta.Outcome != runfolder.OutcomePassed || meta.ScriptName != "Untitled script" {
		t.Errorf("meta = %+v, want a passed run labelled by the default script name", meta)
	}
	if _, err := os.Stat(runfolder.ReportDir(runDir)); !os.IsNotExist(err) {
		t.Errorf("report directory exists although no report was requested: %v", err)
	}
	shots, err := os.ReadDir(runfolder.ArtifactsDir(runDir))
	if err != nil || len(shots) != 1 {
		t.Errorf("artifacts = %v, err %v, want the screenshot written without a report", shots, err)
	}
	if strings.Contains(stderr.String(), "Report: ") {
		t.Errorf("stderr names a report that was not requested: %q", stderr.String())
	}
}

func TestRunScriptFailedRunFinalizesAsFailedAtStep(t *testing.T) {
	root := t.TempDir()
	scriptPath := writeScript(t, "fail.yml", "- sleep: 1\n- assertExpr: { expr: '1 == 2' }\n")

	var stdout, stderr bytes.Buffer
	code := RunScript(context.Background(), mockDeps(&stdout, &stderr), map[string]FlagValue{
		"--file": scriptPath, "--report-dir": root,
	})
	if code != 1 {
		t.Fatalf("exit code %d, want 1", code)
	}
	meta := readMeta(t, singleRunDir(t, root))
	if meta.Result != runfolder.ResultFailed || meta.Outcome != runfolder.OutcomeFailed {
		t.Errorf("meta = result %q outcome %q, want failed failed", meta.Result, meta.Outcome)
	}
	if meta.FailedAtStep == nil || *meta.FailedAtStep != 1 || meta.TotalSteps != 2 {
		t.Errorf("meta step fields = %+v, want failedAtStep 1 of 2 steps", meta)
	}
}

func TestRunScriptJSONCarriesRunFolderAndPrintsNoPathLine(t *testing.T) {
	root := t.TempDir()
	scriptPath := writeScript(t, "flow.yml", "- sleep: 1\n")

	var stdout, stderr bytes.Buffer
	code := RunScript(context.Background(), mockDeps(&stdout, &stderr), map[string]FlagValue{
		"--file": scriptPath, "--report-dir": root, "--json": true,
	})
	if code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr.String())
	}
	var decoded map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("stdout is not JSON: %v", err)
	}
	if decoded["runFolder"] != singleRunDir(t, root) {
		t.Errorf("runFolder = %v, want %q", decoded["runFolder"], singleRunDir(t, root))
	}
	if strings.Contains(stderr.String(), "Run folder: ") {
		t.Errorf("stderr repeats the run folder that the JSON carries: %q", stderr.String())
	}
}

// cancelingFactory cancels the command's context as soon as the device
// is built, which stands in for a stop signal arriving while the run
// is starting.
type cancelingFactory struct {
	device.Factory
	cancel context.CancelCauseFunc
	sig    syscall.Signal
}

func (f cancelingFactory) Build(ctx context.Context, desc device.Descriptor) (device.Built, error) {
	built, err := f.Factory.Build(ctx, desc)
	f.cancel(shutdown.InterruptCause(f.sig))
	return built, err
}

func TestRunScriptStopSignalFinalizesRunFolderAsCancelled(t *testing.T) {
	tests := []struct {
		name     string
		sig      syscall.Signal
		wantCode int
	}{
		{"interrupt", syscall.SIGINT, shutdown.ExitCodeSigint},
		{"terminate", syscall.SIGTERM, shutdown.ExitCodeSigterm},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			scriptPath := writeScript(t, "flow.yml", "- sleep: 1\n")
			ctx, cancel := context.WithCancelCause(context.Background())
			defer cancel(nil)

			var stdout, stderr bytes.Buffer
			deps := mockDeps(&stdout, &stderr)
			deps.Factory = cancelingFactory{Factory: deps.Factory, cancel: cancel, sig: tc.sig}

			code := RunContext(ctx, []string{"run", "--file", scriptPath, "--report-dir", root}, deps)
			if code != tc.wantCode {
				t.Fatalf("exit code %d, want %d; stderr: %s", code, tc.wantCode, stderr.String())
			}
			runDir := singleRunDir(t, root)
			meta := readMeta(t, runDir)
			if meta.Result != runfolder.ResultCancelled || meta.Outcome != runfolder.OutcomeCancelled || meta.CompletedAt == nil {
				t.Errorf("meta = result %q outcome %q completed %v, want a finalized cancelled run", meta.Result, meta.Outcome, meta.CompletedAt)
			}
			if _, err := os.Stat(runfolder.LockPath(runDir)); err != nil {
				t.Errorf("lock file missing: %v", err)
			}
			if runfolder.NewLockLiveness(root).IsLive(meta.RunID) {
				t.Error("the run still holds its lock after the command returned")
			}
		})
	}
}

func TestRunSuiteWritesSuiteKindRunFolder(t *testing.T) {
	root := t.TempDir()
	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, dir, "pass.yml", "- screenshot: home\n")

	var stdout, stderr bytes.Buffer
	code := RunSuite(context.Background(), mockDeps(&stdout, &stderr), map[string]FlagValue{
		"--glob": []string{"*.yml"}, "--report": "html", "--report-dir": root,
	}, nil)
	if code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr.String())
	}

	runDir := singleRunDir(t, root)
	meta := readMeta(t, runDir)
	if meta.Kind != runfolder.KindSuite || meta.Outcome != runfolder.OutcomePassed {
		t.Errorf("meta = kind %q outcome %q, want suite passed", meta.Kind, meta.Outcome)
	}
	shots, err := os.ReadDir(runfolder.ArtifactsDir(runDir))
	if err != nil || len(shots) != 1 {
		t.Fatalf("artifacts = %v, err %v, want one screenshot", shots, err)
	}
	if html := string(findReportHTML(t, root)); !strings.Contains(html, `src="../artifacts/`+shots[0].Name()+`"`) {
		t.Errorf("suite report does not link ../artifacts/%s", shots[0].Name())
	}
}
