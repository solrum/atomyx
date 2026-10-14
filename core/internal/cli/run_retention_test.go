package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dev.atomyx/internal/config"
	"dev.atomyx/internal/device"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/runfolder"
)

func retentionDeps(stderr *bytes.Buffer) Deps {
	clock := orchestra.NewFakeClock(pruneNow.UnixMilli())
	return Deps{
		Probe:   device.NewStaticProbe(nil),
		Factory: device.NewMockDriverFactoryWithDeps(device.MockDriverFactoryDeps{Clock: clock}),
		Clock:   clock,
		Stdout:  &bytes.Buffer{},
		Stderr:  stderr,
	}
}

func TestRunScriptPrunesToTheConfiguredRetentionBeforeWriting(t *testing.T) {
	root, ids := seedPruneStore(t)
	writeRetentionConfig(t, "retention:\n  keepLast: 1\n")
	script := filepath.Join(t.TempDir(), "flow.yml")
	if err := os.WriteFile(script, []byte("- sleep: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer

	code := RunScript(context.Background(), retentionDeps(&stderr), map[string]FlagValue{"--file": script})

	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	if runFolderExists(root, ids["ancient"]) || runFolderExists(root, ids["old"]) {
		t.Error("runs outside keepLast were not pruned")
	}
	if !runFolderExists(root, ids["recent"]) {
		t.Error("the newest existing run was pruned")
	}
	listing, _, err := runfolder.NewReader(root).List()
	if err != nil {
		t.Fatal(err)
	}
	if len(listing) != 2 {
		t.Errorf("runs after the command = %d, want keepLast plus the new run", len(listing))
	}
}

func TestRunScriptWithoutRetentionPrunesNothing(t *testing.T) {
	root, ids := seedPruneStore(t)
	writeRetentionConfig(t, "")
	script := filepath.Join(t.TempDir(), "flow.yml")
	if err := os.WriteFile(script, []byte("- sleep: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer

	RunScript(context.Background(), retentionDeps(&stderr), map[string]FlagValue{"--file": script})

	for name, id := range ids {
		if !runFolderExists(root, id) {
			t.Errorf("run %s was pruned without a retention policy", name)
		}
	}
}

func TestSuiteWorkerNeverPrunes(t *testing.T) {
	root, ids := seedPruneStore(t)
	writeRetentionConfig(t, "retention:\n  keepLast: 1\n")
	var stderr bytes.Buffer

	RunSuite(context.Background(), retentionDeps(&stderr), map[string]FlagValue{"--worker": true, "--device": "d1"}, nil)

	for name, id := range ids {
		if !runFolderExists(root, id) {
			t.Errorf("a --worker process pruned run %s", name)
		}
	}
}

func TestPruneToRetentionPrintsOneWarningLinePerKeptLiveRun(t *testing.T) {
	root := runsStorageDir(t)
	seedRecordedRun(t, root, "recent", daysBeforePruneNow(2), time.Second, runfolder.OutcomePassed, runfolder.Meta{TestKey: "a"})
	old := daysBeforePruneNow(60)
	live, err := runfolder.NewStarter(root, func() time.Time { return old }, bytes.NewReader([]byte{1, 2})).Start("live", runfolder.Meta{Kind: runfolder.KindScript})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = live.Close() })
	keep := int64(1)
	loaded := &config.LoadedConfig{Config: config.AtomyxConfig{Retention: &config.Retention{KeepLast: &keep}}}
	var stderr bytes.Buffer

	pruneToRetention(retentionDeps(&stderr), loaded, root)

	lines := strings.Split(strings.TrimSpace(stderr.String()), "\n")
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "warning: retention prune: ") || !strings.Contains(lines[0], "still being written") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if !runFolderExists(root, live.ID()) {
		t.Error("a live run was deleted")
	}
}

func TestPruneToRetentionIsSilentWithoutAPolicy(t *testing.T) {
	var stderr bytes.Buffer

	pruneToRetention(retentionDeps(&stderr), nil, t.TempDir())
	pruneToRetention(retentionDeps(&stderr), &config.LoadedConfig{}, t.TempDir())

	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
}
