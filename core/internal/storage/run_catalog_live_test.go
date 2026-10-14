package storage

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"dev.atomyx/internal/runfolder"
)

const childRunRootEnv = "ATOMYX_STORAGE_TEST_RUN_ROOT"

// TestMain re-executes the test binary as a second process that starts
// a run folder, prints its id, and blocks until stdin closes, so the
// run is held live by a writer outside the test process.
func TestMain(m *testing.M) {
	if root := os.Getenv(childRunRootEnv); root != "" {
		os.Exit(runWriterChild(root))
	}
	os.Exit(m.Run())
}

func runWriterChild(root string) int {
	w, err := runfolder.NewStarter(root, time.Now, rand.Reader).Start("forked", runfolder.Meta{Kind: runfolder.KindScript})
	if err != nil {
		fmt.Println("error: " + err.Error())
		return 2
	}
	fmt.Println(w.ID())
	_, _ = io.Copy(io.Discard, os.Stdin)
	return 0
}

func startWriterChild(t *testing.T, root string) string {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), os.Args[0])
	cmd.Env = append(os.Environ(), childRunRootEnv+"="+root)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	line, err := bufio.NewReader(stdout).ReadString('\n')
	id := strings.TrimSpace(line)
	if err != nil || strings.HasPrefix(id, "error:") {
		t.Fatalf("child did not start a run: %q, %v", line, err)
	}
	return id
}

func liveCatalog(t *testing.T, root string) *RunCatalog {
	t.Helper()
	fs, err := NewFileStorage(FileStorageOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	return NewRunCatalog(fs, root, runfolder.NewLockLiveness(root))
}

func TestRunCatalogDeleteRefusesARunThisProcessIsWriting(t *testing.T) {
	root := t.TempDir()
	c := liveCatalog(t, root)
	w, err := runfolder.NewStarter(root, time.Now, bytes.NewReader([]byte{1, 2})).Start("mine", runfolder.Meta{Kind: runfolder.KindScript})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })

	removed, err := c.Delete(w.ID())

	if removed || !errors.Is(err, ErrRunLive) {
		t.Fatalf("Delete of a live run = %v, %v, want ErrRunLive", removed, err)
	}
	if _, statErr := os.Stat(w.Dir()); statErr != nil {
		t.Fatalf("run folder was touched: %v", statErr)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if removed, err := c.Delete(w.ID()); !removed || err != nil {
		t.Fatalf("Delete after the writer closed = %v, %v", removed, err)
	}
}

func TestRunCatalogDeleteRefusesARunAnotherProcessIsWriting(t *testing.T) {
	root := t.TempDir()
	c := liveCatalog(t, root)
	id := startWriterChild(t, root)

	removed, err := c.Delete(id)

	if removed || !errors.Is(err, ErrRunLive) {
		t.Fatalf("Delete of a run another process writes = %v, %v, want ErrRunLive", removed, err)
	}
	if _, statErr := os.Stat(runfolder.RunDir(root, id)); statErr != nil {
		t.Fatalf("run folder was touched: %v", statErr)
	}
}

func TestDeletePruneUnitRefusesAUnitHoldingARunAnotherProcessWrites(t *testing.T) {
	root := t.TempDir()
	c := liveCatalog(t, root)
	liveID := startWriterChild(t, root)
	suite := finishedMeta("suite", 30*pruneDay)
	writePruneRun(t, root, suite)
	unit := PruneUnit{Root: suite, Members: []string{"suite", liveID}}

	err := c.DeletePruneUnit(unit)

	if !errors.Is(err, ErrRunLive) {
		t.Fatalf("DeletePruneUnit = %v, want ErrRunLive", err)
	}
	if !runExists(root, "suite") || !runExists(root, liveID) {
		t.Error("a unit holding a live run lost a member")
	}
}

func TestPlanPruneFlagsAUnitWhoseWorkerThisProcessIsWriting(t *testing.T) {
	root := t.TempDir()
	c := liveCatalog(t, root)
	writePruneRun(t, root, withKind(finishedMeta("suite", 30*pruneDay), runfolder.KindSuite))
	w, err := runfolder.NewStarter(root, func() time.Time { return pruneNow.Add(-30 * pruneDay) }, bytes.NewReader([]byte{3, 4})).
		Start("worker", runfolder.Meta{Kind: runfolder.KindScript, ParentRunID: "suite"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })

	plan, err := c.PlanPrune(PruneCriteria{OlderThan: pruneDay, Now: pruneNow})

	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Units) != 1 || !plan.Units[0].Live {
		t.Fatalf("units = %+v, want one live unit", plan.Units)
	}
	if err := c.DeletePruneUnit(plan.Units[0]); !errors.Is(err, ErrRunLive) {
		t.Fatalf("DeletePruneUnit = %v, want ErrRunLive", err)
	}
	if !runExists(root, "suite") || !runExists(root, w.ID()) {
		t.Error("a unit with a live worker lost a member")
	}
}
