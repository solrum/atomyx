package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/storage"
)

func seedLegacyRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	store, err := storage.NewFileStorage(storage.FileStorageOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []storage.PersistedRun{
		{ID: "a", Name: "a", Source: "agent", Status: storage.RunStatusPassed, StartedAt: 1},
		{ID: "b", Name: "b", Source: "agent", Status: storage.RunStatusRunning, StartedAt: 2},
	} {
		if err := store.Save(storage.RunKey(r.ID), r); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func runMigrate(t *testing.T, root string, extra ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	flags := map[string]FlagValue{"--migrate-runs": true, "--report-dir": root}
	for _, name := range extra {
		flags[name] = true
	}
	code := Doctor(context.Background(), Deps{Stdout: &stdout, Stderr: &stderr}, flags)
	return code, stdout.String(), stderr.String()
}

func TestDoctorMigrateRunsPrintsTheSummaryAndIsIdempotent(t *testing.T) {
	root := seedLegacyRoot(t)

	code, out, errOut := runMigrate(t, root)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	for _, want := range []string{"migrated:", "2", "already migrated:", "conflicts:", "damaged:"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q lacks %q", out, want)
		}
	}
	for _, id := range []string{"a", "b"} {
		if _, ok, err := runfolder.NewReader(root).Get(id); !ok || err != nil {
			t.Fatalf("%s: no folder: %v %v", id, ok, err)
		}
	}

	code, out, _ = runMigrate(t, root, "--json")
	if code != 0 {
		t.Fatalf("second run exit %d", code)
	}
	var got struct {
		Root            string           `json:"root"`
		Migrated        []string         `json:"migrated"`
		AlreadyMigrated []string         `json:"alreadyMigrated"`
		Conflicts       []map[string]any `json:"conflicts"`
		Damaged         []map[string]any `json:"damaged"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("json output %q: %v", out, err)
	}
	if got.Root != root || got.Migrated == nil || len(got.Migrated) != 0 || got.AlreadyMigrated == nil || got.Conflicts == nil || got.Damaged == nil {
		t.Fatalf("second run report = %+v, want empty non-null lists", got)
	}
}

func TestDoctorMigrateRunsExitsOneOnDamageAndConflict(t *testing.T) {
	root := seedLegacyRoot(t)
	if err := os.WriteFile(filepath.Join(root, "runs", "broken.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out, _ := runMigrate(t, root)
	if code != 1 || !strings.Contains(out, "broken") {
		t.Fatalf("exit %d, output %q, want 1 naming the damaged record", code, out)
	}
	if _, ok, _ := runfolder.NewReader(root).Get("a"); !ok {
		t.Fatal("healthy records were not migrated alongside the damaged one")
	}
	code, out, _ = runMigrate(t, root, "--json")
	var got struct {
		Damaged []struct {
			ID     string `json:"id"`
			Reason string `json:"reason"`
		} `json:"damaged"`
	}
	if code != 1 || json.Unmarshal([]byte(out), &got) != nil || len(got.Damaged) != 1 || got.Damaged[0].ID != "broken" || got.Damaged[0].Reason == "" {
		t.Fatalf("json exit %d output %q", code, out)
	}
}

func TestDoctorMigrateRunsRefusesDeviceFlagsAndNeedsNoDevice(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Doctor(context.Background(), Deps{Stdout: &stdout, Stderr: &stderr},
		map[string]FlagValue{"--migrate-runs": true, "--platform": "ios", "--report-dir": t.TempDir()})
	if code != 2 || !strings.Contains(stderr.String(), "--migrate-runs") {
		t.Fatalf("exit %d, stderr %q, want 2 explaining the combination", code, stderr.String())
	}
}

func TestDoctorMigrateRunsOnAnEmptyRootChangesNothing(t *testing.T) {
	root := t.TempDir()
	code, _, errOut := runMigrate(t, root)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
}

func TestDoctorFlagTableAcceptsMigrateRuns(t *testing.T) {
	argv, err := ParseArgv([]string{"doctor", "--migrate-runs", "--report-dir", "/x", "--json"})
	if err != nil || !flagBool(argv.Flags, "--migrate-runs") {
		t.Fatalf("ParseArgv = %+v %v", argv, err)
	}
}
