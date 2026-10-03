package cli

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/lease"
	"dev.atomyx/internal/runfolder"
)

const (
	bundleSecret    = "s3cr3t-value-from-env"
	deviceEnvSecret = "TEAM-ABCDE12345"
)

type fakeProbe struct{ devices []device.Descriptor }

func (p fakeProbe) Scan(context.Context) []device.Descriptor { return p.devices }

type fakeLeases struct {
	status lease.Status
	err    error
	probed []lease.Key
}

func (f *fakeLeases) Acquire(context.Context, lease.Key, lease.Holder) (*lease.Lease, error) {
	return nil, errors.New("a bundle must never acquire a lease")
}
func (f *fakeLeases) Dir() string { return "" }
func (f *fakeLeases) Probe(_ context.Context, key lease.Key) (lease.Status, error) {
	f.probed = append(f.probed, key)
	return f.status, f.err
}

func seedRun(t *testing.T, root, id string, startedAt int64, logBody string) string {
	t.Helper()
	dir := runfolder.RunDir(root, id)
	artifacts := runfolder.ArtifactsDir(dir)
	if err := os.MkdirAll(artifacts, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := runfolder.Meta{SchemaVersion: runfolder.SchemaVersion, RunID: id, Kind: runfolder.KindScript, ScriptName: "login " + bundleSecret, StartedAt: startedAt}
	body, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		filepath.Join(dir, runfolder.MetaFileName):                                         string(body),
		filepath.Join(dir, runfolder.LogFileName):                                          logBody,
		filepath.Join(dir, "step-001-log.jsonl"):                                           "step " + bundleSecret + "\n",
		filepath.Join(artifacts, "step-002-log.jsonl"):                                     "artifact log\n",
		filepath.Join(artifacts, "script-001-step-003-log.jsonl"):                          "session log\n",
		filepath.Join(artifacts, evidencePrefix("Login flow: happy")+"step-004-log.jsonl"): "child log\n",
		filepath.Join(artifacts, "step-002-tree.json"):                                     "{}",
		filepath.Join(artifacts, "script-001-step-003-failure.png"):                        "PNGDATA",
		filepath.Join(artifacts, "step-001-failure.png"):                                   "PNGDATA",
		filepath.Join(artifacts, "tree.json"):                                              "{}",
		filepath.Join(dir, runfolder.StepsFileName):                                        "{}\n",
		filepath.Join(runfolder.ReportDir(dir), "screenshot.png"):                          "PNGDATA",
	}
	for p, c := range files {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func readZip(t *testing.T, path string) map[string]string {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = zr.Close() }()
	out := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		out[f.Name] = string(b)
	}
	return out
}

func bundleDeps(leases *fakeLeases, hostCmd HostCommand) (Deps, *bytes.Buffer, *bytes.Buffer) {
	var stdout, stderr bytes.Buffer
	deps := Deps{
		Probe:       fakeProbe{devices: []device.Descriptor{{ID: "emulator-5554", Platform: device.PlatformAndroid, Name: "Pixel", Kind: device.KindEmulator, State: device.StateOnline}}},
		Stdout:      &stdout,
		Stderr:      &stderr,
		HostCommand: hostCmd,
	}
	deps.Session.Leases = leases
	return deps, &stdout, &stderr
}

func TestBundleKeepsFiveNewestRunsAndScrubsSecrets(t *testing.T) {
	root := t.TempDir()
	for i := 1; i <= 6; i++ {
		id := "20261002-10000" + string(rune('0'+i)) + "-run-aaaa"
		seedRun(t, root, id, int64(1000*i), "log line "+bundleSecret+" token "+deviceEnvSecret+"\n")
	}
	searchDir := t.TempDir()
	envDir := filepath.Join(searchDir, filepath.Dir(iosDeviceEnvPath))
	if err := os.MkdirAll(envDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(searchDir, iosDeviceEnvPath), []byte("DEV_TEAM="+deviceEnvSecret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	holder := lease.Holder{PID: 42, Command: "atomyx run", Role: "cli"}
	leases := &fakeLeases{status: lease.Status{Held: true, Holder: holder, HolderKnown: true}}
	deps, _, _ := bundleDeps(leases, func(_ context.Context, name string, _ ...string) (string, error) {
		if name == "adb" {
			return "Android Debug Bridge version 1.0.41\nextra\n", nil
		}
		return "", errors.New("executable file not found")
	})
	out := filepath.Join(t.TempDir(), "bundle.zip")
	opts := bundleOptions{Output: out, Root: root, Environ: []string{"API_TOKEN=" + bundleSecret, "HOME=/Users/x"}, EnvSearchDir: filepath.Join(searchDir, "sub")}
	if err := os.MkdirAll(opts.EnvSearchDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeBundle(context.Background(), deps, opts); err != nil {
		t.Fatal(err)
	}

	files := readZip(t, out)
	runDirs := map[string]bool{}
	for name, body := range files {
		if strings.Contains(body, bundleSecret) || strings.Contains(body, deviceEnvSecret) {
			t.Errorf("%s still holds a secret", name)
		}
		if strings.HasSuffix(name, ".png") || strings.HasSuffix(name, "tree.json") || strings.HasSuffix(name, runfolder.StepsFileName) {
			t.Errorf("%s must not be bundled", name)
		}
		if parts := strings.Split(name, "/"); parts[0] == "runs" {
			runDirs[parts[1]] = true
		}
	}
	if len(runDirs) != bundleRunCount {
		t.Fatalf("got %d runs %v, want %d", len(runDirs), runDirs, bundleRunCount)
	}
	if runDirs["20261002-100001-run-aaaa"] {
		t.Error("the oldest run must be left out")
	}
	newest := "runs/20261002-100006-run-aaaa/"
	for _, name := range []string{
		runfolder.MetaFileName, runfolder.LogFileName, "step-001-log.jsonl",
		"artifacts/step-002-log.jsonl",
		"artifacts/script-001-step-003-log.jsonl",
		"artifacts/login-flow-happy-step-004-log.jsonl",
	} {
		if _, ok := files[newest+name]; !ok {
			t.Errorf("missing %s%s", newest, name)
		}
	}
	if !strings.Contains(files[newest+runfolder.LogFileName], "[REDACTED]") {
		t.Error("the log must carry the mask")
	}

	var devices []bundleDeviceJSON
	if err := json.Unmarshal([]byte(files["devices.json"]), &devices); err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 || devices[0].Lease.State != "held" || devices[0].Lease.Holder == nil || devices[0].Lease.Holder.PID != 42 || devices[0].HealthChecked {
		t.Fatalf("devices.json: %+v", devices)
	}
	if len(leases.probed) != 1 {
		t.Fatalf("probed %d leases", len(leases.probed))
	}

	var env environmentJSON
	if err := json.Unmarshal([]byte(files["environment.json"]), &env); err != nil {
		t.Fatal(err)
	}
	if !env.Tools["adb"].Found || strings.Count(env.Tools["adb"].Version, "\n") != 1 {
		t.Errorf("adb: %+v", env.Tools["adb"])
	}
	if env.Tools["iproxy"].Found || env.Tools["iproxy"].Error == "" {
		t.Errorf("a missing tool must be recorded as missing: %+v", env.Tools["iproxy"])
	}
}

func TestBundleBoundsHostToolQueries(t *testing.T) {
	deps, _, _ := bundleDeps(&fakeLeases{}, func(ctx context.Context, _ string, _ ...string) (string, error) {
		dl, ok := ctx.Deadline()
		if !ok || time.Until(dl) > hostToolTimeout {
			t.Errorf("tool query lacks the %s bound", hostToolTimeout)
		}
		return "v1", nil
	})
	collectEnvironment(context.Background(), deps)
}

func TestBundleLeaseProbeFailureIsRecordedNotFatal(t *testing.T) {
	deps, _, _ := bundleDeps(&fakeLeases{err: errors.New("boom")}, nil)
	devices := collectBundleDevices(context.Background(), deps)
	if len(devices) != 1 || devices[0].Lease.State != "unknown" || devices[0].Lease.Error != "boom" {
		t.Fatalf("got %+v", devices)
	}
}

func TestBundleFreeLeaseAndNoHostCommand(t *testing.T) {
	deps, _, _ := bundleDeps(&fakeLeases{}, nil)
	devices := collectBundleDevices(context.Background(), deps)
	if devices[0].Lease.State != "free" {
		t.Fatalf("got %+v", devices[0].Lease)
	}
	env := collectEnvironment(context.Background(), deps)
	if env.Tools["adb"].Found {
		t.Fatal("no host command means no tool is found")
	}
}

func TestBundleKeepsTheTailOfAnOversizedLog(t *testing.T) {
	p := filepath.Join(t.TempDir(), "log.jsonl")
	body := append(bytes.Repeat([]byte("a"), maxBundleFileBytes), []byte("TAIL")...)
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok := readBundleFile(p)
	if !ok || len(got) != maxBundleFileBytes || !bytes.HasSuffix(got, []byte("TAIL")) {
		t.Fatalf("ok=%v len=%d", ok, len(got))
	}
}

func TestBundleSkipsSymbolicLinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "log.jsonl")
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symlinks unavailable")
	}
	if _, ok := readBundleFile(link); ok {
		t.Fatal("a symbolic link must not be followed")
	}
}

func TestDoctorBundleWritesZipAndPrintsPath(t *testing.T) {
	root := t.TempDir()
	seedRun(t, root, "20261002-100001-run-aaaa", 1, "log\n")
	deps, stdout, stderr := bundleDeps(&fakeLeases{}, nil)
	out := filepath.Join(t.TempDir(), "b.zip")
	code := Doctor(context.Background(), deps, map[string]FlagValue{bundleFlag: out, "--report-dir": root})
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != out {
		t.Fatalf("stdout %q, want %q", stdout.String(), out)
	}
	if _, ok := readZip(t, out)["runs/20261002-100001-run-aaaa/meta.json"]; !ok {
		t.Fatal("run meta missing from the zip")
	}
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(out), ".atomyx-bundle-*"))
	if len(leftovers) != 0 {
		t.Fatalf("temporary files left behind: %v", leftovers)
	}
}

func TestDoctorBundleWriteFailureExitsOneAndLeavesNoFile(t *testing.T) {
	deps, _, stderr := bundleDeps(&fakeLeases{}, nil)
	missingDir := filepath.Join(t.TempDir(), "absent", "b.zip")
	code := Doctor(context.Background(), deps, map[string]FlagValue{bundleFlag: missingDir, "--report-dir": t.TempDir()})
	if code != 1 || !strings.Contains(stderr.String(), "could not write the bundle") {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
}

func TestDoctorBundleDoesNotReplaceTargetOnFailure(t *testing.T) {
	deps, _, _ := bundleDeps(&fakeLeases{}, nil)
	dir := t.TempDir()
	target := filepath.Join(dir, "b.zip")
	if err := os.MkdirAll(filepath.Join(target, "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	code := Doctor(context.Background(), deps, map[string]FlagValue{bundleFlag: target, "--report-dir": t.TempDir()})
	if code != 1 {
		t.Fatalf("exit %d, want 1 when the target is a non-empty directory", code)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("temporary file left behind: %v", entries)
	}
}

func TestDoctorBundleMisuse(t *testing.T) {
	cases := []struct {
		name  string
		flags map[string]FlagValue
		want  string
	}{
		{"with migrate-runs", map[string]FlagValue{bundleFlag: "x.zip", migrateRunsFlag: true}, migrateRunsFlag},
		{"with platform", map[string]FlagValue{bundleFlag: "x.zip", "--platform": "ios"}, "--platform"},
		{"with device", map[string]FlagValue{bundleFlag: "x.zip", "--device": "d"}, "--device"},
		{"with json", map[string]FlagValue{bundleFlag: "x.zip", "--json": true}, "--json"},
		{"empty path", map[string]FlagValue{bundleFlag: ""}, "needs the path"},
		{"boolean form", map[string]FlagValue{bundleFlag: true}, "needs the path"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			deps, _, stderr := bundleDeps(&fakeLeases{}, nil)
			if code := Doctor(context.Background(), deps, c.flags); code != 2 {
				t.Fatalf("exit %d, want 2", code)
			}
			if !strings.Contains(stderr.String(), c.want) {
				t.Fatalf("stderr %q lacks %q", stderr.String(), c.want)
			}
		})
	}
}

func TestDoctorBundleFlagParses(t *testing.T) {
	argv, err := ParseArgv([]string{"doctor", "--bundle", "out.zip", "--report-dir", "/x"})
	if err != nil {
		t.Fatal(err)
	}
	if argv.Flags[bundleFlag] != "out.zip" {
		t.Fatalf("flags %#v", argv.Flags)
	}
}

func TestDeviceEnvValuesWalksUpward(t *testing.T) {
	top := t.TempDir()
	if err := os.MkdirAll(filepath.Join(top, filepath.Dir(iosDeviceEnvPath)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(top, iosDeviceEnvPath), []byte("DEVICE_UDID=00008030-AAAABBBB\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	deep := filepath.Join(top, "a", "b")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deep, deviceEnvName), []byte("OTHER=local-value-1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := deviceEnvValues(deep)
	if len(got) != 2 {
		t.Fatalf("got %v", got)
	}
}
