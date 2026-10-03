package cli

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/lease"
	"dev.atomyx/internal/redact"
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/storageroot"
	"dev.atomyx/internal/version"
)

const (
	// bundleFlag selects the diagnostic bundle mode of doctor.
	bundleFlag = "--bundle"

	// bundleRunCount is how many of the newest runs a bundle includes.
	bundleRunCount = 5

	// hostToolTimeout bounds each host tool version query so a hung tool
	// cannot stall the bundle.
	hostToolTimeout = 5 * time.Second

	// maxBundleFileBytes caps one file in the bundle. A larger log keeps
	// its newest bytes, because the end of a log explains a failure.
	maxBundleFileBytes = 4 << 20

	// maxToolVersionLines caps how many output lines describe one tool.
	maxToolVersionLines = 5
)

// iosDeviceEnvPath is where a contributor's iOS signing and device
// configuration lives, relative to a checkout root.
var iosDeviceEnvPath = filepath.Join("platforms", "ios-agent", "device.env")

// deviceEnvName is the bare file name of that configuration.
const deviceEnvName = "device.env"

// stepLogPattern matches the per-step log files of a run.
var stepLogPattern = regexp.MustCompile(`^step-\d+-log\.jsonl$`)

// bundleTools are the host tools whose versions a bundle records, with
// the arguments that print the version.
var bundleTools = []struct {
	name string
	args []string
}{
	{"adb", []string{"version"}},
	{"xcodebuild", []string{"-version"}},
	{"iproxy", []string{"--version"}},
	{"go", []string{"version"}},
}

// HostCommand runs a host tool and returns its combined output. A tool
// that is absent or fails yields an error.
type HostCommand func(ctx context.Context, name string, args ...string) (string, error)

// leaseProber is the read-only lease query a bundle needs. The lease
// manager satisfies it without creating or writing a lease file.
type leaseProber interface {
	Probe(ctx context.Context, key lease.Key) (lease.Status, error)
}

type toolJSON struct {
	Found   bool   `json:"found"`
	Version string `json:"version,omitempty"`
	Error   string `json:"error,omitempty"`
}

type environmentJSON struct {
	Atomyx string              `json:"atomyx"`
	GOOS   string              `json:"goos"`
	GOARCH string              `json:"goarch"`
	Tools  map[string]toolJSON `json:"tools"`
}

type bundleLeaseJSON struct {
	State  string      `json:"state"`
	Holder *holderJSON `json:"holder,omitempty"`
	Error  string      `json:"error,omitempty"`
}

type bundleDeviceJSON struct {
	Platform      string          `json:"platform"`
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Kind          string          `json:"kind"`
	State         string          `json:"state"`
	HealthChecked bool            `json:"healthChecked"`
	Lease         bundleLeaseJSON `json:"lease"`
}

// bundleOptions are the host facts a bundle reads, so a test can
// substitute each one.
type bundleOptions struct {
	Output       string
	Root         string
	Environ      []string
	EnvSearchDir string
}

// doctorBundle writes a diagnostic zip of the host, the devices and the
// newest runs to the path in --bundle, and prints that path.
//
// The bundle covers every device the host sees, so --platform, --device
// and --json are refused. --report-dir picks the run root, as it does for
// --migrate-runs. No device lease is acquired, so a device another
// process drives is reported with its holder and its health is not
// checked.
//
// Exit codes: 0 when the bundle is written, 1 when it could not be
// written, and 2 when the command is misused.
func doctorBundle(ctx context.Context, deps Deps, flags map[string]FlagValue) int {
	stderr := deps.stderr()
	if flagBool(flags, migrateRunsFlag) {
		_, _ = fmt.Fprintf(stderr, "error: %s writes a diagnostic bundle, so it cannot be combined with %s.\n", bundleFlag, migrateRunsFlag)
		return 2
	}
	for _, name := range append([]string{"--json"}, deviceFlags...) {
		if _, set := flags[name]; set {
			_, _ = fmt.Fprintf(stderr, "error: %s covers every device and always writes a zip, so it cannot be combined with %s.\n", bundleFlag, name)
			return 2
		}
	}
	output, _ := flagString(flags, bundleFlag)
	if output == "" {
		_, _ = fmt.Fprintf(stderr, "error: %s needs the path of the zip file to write.\n", bundleFlag)
		return 2
	}
	explicit, _ := flagString(flags, "--report-dir")
	root, err := storageroot.Resolve(explicit)
	if err != nil {
		return reportUncaught(ctx, stderr, deps.Debug, err)
	}
	cwd, _ := os.Getwd()
	abs, err := filepath.Abs(output)
	if err != nil {
		return reportUncaught(ctx, stderr, deps.Debug, err)
	}
	opts := bundleOptions{Output: abs, Root: root, Environ: os.Environ(), EnvSearchDir: cwd}
	if err := writeBundle(ctx, deps, opts); err != nil {
		_, _ = fmt.Fprintf(stderr, "error: could not write the bundle: %v\n", err)
		return 1
	}
	_, _ = fmt.Fprintln(deps.stdout(), abs)
	return 0
}

// writeBundle collects the bundle, scrubs every file and writes the zip
// atomically: a temporary file in the target directory, then a rename.
func writeBundle(ctx context.Context, deps Deps, opts bundleOptions) error {
	redactor := redact.New(append(redact.EnvValues(opts.Environ), deviceEnvValues(opts.EnvSearchDir)...))

	files, err := collectBundleFiles(ctx, deps, opts)
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(opts.Output), ".atomyx-bundle-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	zw := zip.NewWriter(tmp)
	for _, f := range files {
		w, err := zw.Create(f.name)
		if err == nil {
			_, err = w.Write(redactor.Bytes(f.body))
		}
		if err != nil {
			_ = tmp.Close()
			return err
		}
	}
	if err := zw.Close(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, opts.Output)
}

type bundleFile struct {
	name string
	body []byte
}

func collectBundleFiles(ctx context.Context, deps Deps, opts bundleOptions) ([]bundleFile, error) {
	var files []bundleFile
	add := func(name string, v any) error {
		var buf bytes.Buffer
		if err := encodeJSON(&buf, v); err != nil {
			return err
		}
		files = append(files, bundleFile{name: name, body: buf.Bytes()})
		return nil
	}
	if err := add("environment.json", collectEnvironment(ctx, deps)); err != nil {
		return nil, err
	}
	if err := add("devices.json", collectBundleDevices(ctx, deps)); err != nil {
		return nil, err
	}
	files = append(files, collectRunFiles(opts.Root)...)
	sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })
	return files, nil
}

func collectEnvironment(ctx context.Context, deps Deps) environmentJSON {
	env := environmentJSON{
		Atomyx: version.Read().String(),
		GOOS:   runtime.GOOS,
		GOARCH: runtime.GOARCH,
		Tools:  make(map[string]toolJSON, len(bundleTools)),
	}
	for _, tool := range bundleTools {
		if deps.HostCommand == nil {
			env.Tools[tool.name] = toolJSON{Error: "tool queries are not available"}
			continue
		}
		toolCtx, cancel := context.WithTimeout(ctx, hostToolTimeout)
		out, err := deps.HostCommand(toolCtx, tool.name, tool.args...)
		cancel()
		if err != nil {
			env.Tools[tool.name] = toolJSON{Error: err.Error()}
			continue
		}
		env.Tools[tool.name] = toolJSON{Found: true, Version: firstLines(out, maxToolVersionLines)}
	}
	return env
}

func firstLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

func collectBundleDevices(ctx context.Context, deps Deps) []bundleDeviceJSON {
	out := []bundleDeviceJSON{}
	if deps.Probe == nil {
		return out
	}
	for _, d := range deps.Probe.Scan(ctx) {
		out = append(out, bundleDeviceJSON{
			Platform: string(d.Platform),
			ID:       d.ID,
			Name:     d.Name,
			Kind:     string(d.Kind),
			State:    displayState(d),
			Lease:    probeLease(ctx, deps, d),
		})
	}
	return out
}

func probeLease(ctx context.Context, deps Deps, d device.Descriptor) bundleLeaseJSON {
	prober, ok := deps.Session.Leases.(leaseProber)
	if !ok {
		return bundleLeaseJSON{State: "unknown", Error: "lease state is not available"}
	}
	identity, err := deps.Session.ResolveIdentity(ctx, d)
	if err != nil {
		return bundleLeaseJSON{State: "unknown", Error: err.Error()}
	}
	status, err := prober.Probe(ctx, lease.KeyFor(string(identity.Platform), identity.HardwareID))
	if err != nil {
		return bundleLeaseJSON{State: "unknown", Error: err.Error()}
	}
	if !status.Held {
		return bundleLeaseJSON{State: "free"}
	}
	held := bundleLeaseJSON{State: "held"}
	if status.HolderKnown {
		h := status.Holder
		held.Holder = &holderJSON{PID: h.PID, Command: h.Command, Role: h.Role, State: h.State(), RunID: h.RunID}
	}
	return held
}

// collectRunFiles returns the meta and log files of the newest runs. A
// run is skipped silently when its folder cannot be read, because a
// damaged run must not stop the bundle.
func collectRunFiles(root string) []bundleFile {
	metas, _, err := runfolder.NewReader(root).List()
	if err != nil {
		return nil
	}
	sort.Slice(metas, func(i, j int) bool {
		if metas[i].StartedAt != metas[j].StartedAt {
			return metas[i].StartedAt > metas[j].StartedAt
		}
		return metas[i].RunID > metas[j].RunID
	})
	if len(metas) > bundleRunCount {
		metas = metas[:bundleRunCount]
	}
	runsName := filepath.Base(runfolder.RunsDir(root))
	var files []bundleFile
	for _, m := range metas {
		runDir := runfolder.RunDir(root, m.RunID)
		prefix := path.Join(runsName, m.RunID)
		artifacts := runfolder.ArtifactsDir(runDir)
		for _, name := range []string{runfolder.MetaFileName, runfolder.LogFileName} {
			if body, ok := readBundleFile(filepath.Join(runDir, name)); ok {
				files = append(files, bundleFile{name: path.Join(prefix, name), body: body})
			}
		}
		for _, dir := range []string{runDir, artifacts} {
			rel := ""
			if dir == artifacts {
				rel = filepath.Base(artifacts)
			}
			files = append(files, stepLogFiles(dir, path.Join(prefix, rel))...)
		}
	}
	return files
}

func stepLogFiles(dir, prefix string) []bundleFile {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var files []bundleFile
	for _, e := range entries {
		if !e.Type().IsRegular() || !stepLogPattern.MatchString(e.Name()) {
			continue
		}
		if body, ok := readBundleFile(filepath.Join(dir, e.Name())); ok {
			files = append(files, bundleFile{name: path.Join(prefix, e.Name()), body: body})
		}
	}
	return files
}

// readBundleFile reads a regular file, keeping only its last
// maxBundleFileBytes. A symbolic link or an unreadable file reports false
// so the bundle never follows a link out of the run folder.
func readBundleFile(p string) ([]byte, bool) {
	info, err := os.Lstat(p)
	if err != nil || !info.Mode().IsRegular() {
		return nil, false
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, false
	}
	defer func() { _ = f.Close() }()
	if info.Size() > maxBundleFileBytes {
		if _, err := f.Seek(info.Size()-maxBundleFileBytes, io.SeekStart); err != nil {
			return nil, false
		}
	}
	body, err := io.ReadAll(io.LimitReader(f, maxBundleFileBytes))
	if err != nil {
		return nil, false
	}
	return body, true
}

// deviceEnvValues returns the values of every device.env file found at
// or above start, in the iOS agent folder of a checkout or directly in a
// directory. The values are never printed, only masked.
func deviceEnvValues(start string) []string {
	var values []string
	for dir := start; dir != "" && dir != string(filepath.Separator); dir = filepath.Dir(dir) {
		for _, candidate := range []string{filepath.Join(dir, iosDeviceEnvPath), filepath.Join(dir, deviceEnvName)} {
			if body, err := os.ReadFile(candidate); err == nil {
				values = append(values, redact.EnvFileValues(body)...)
			}
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	return values
}
