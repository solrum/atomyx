# Atomyx: Architecture reference

## Purpose

Who reads this: a contributor or AI agent about to design or evaluate
a change to the driver, orchestra, script, MCP, or CLI layers.

When to read it: before adding a Driver method, a script step, an
MCP tool, or a new platform adapter, and before judging whether a
change respects the module's dependency direction.

What this does not cover: concrete file locations (see
[`repo-map.md`](./repo-map.md)), build and test commands (see
[`development.md`](./development.md)), and the MCP tool-by-tool
contract (see [`tools.md`](./tools.md)).

## TL;DR

Atomyx's runtime is a single Go module (`dev.atomyx`) under `core/`.
One platform-neutral `Driver` port abstracts device control. Four
implementers satisfy it (mock, Android, iOS device, iOS simulator).
An `Orchestra` layer composes `Driver` calls into selector
resolution, scroll-into-view, obscurement handling, and wait
primitives, so every higher layer inherits that behavior for free.
The script engine and the MCP server are both built on `Orchestra`,
never on a `Driver` directly. Dependency direction between all of
this is enforced by `go-arch-lint` reading `core/.go-arch-lint.yml`,
run locally through `make check`.

## Layers

```mermaid
flowchart TB
    subgraph leaves["Leaf packages"]
        wire["wire (shared types)"]
        diag["diag (error codes)"]
        clock["clock"]
        flock["flock (file lock primitive)"]
        lease["lease (per-device flock)"]
        logging["logging"]
        transport["transport (HTTP + TCP clients)"]
        process["process (adb/xcrun execution)"]
    end

    driver["driver (Driver port + Capabilities)"]
    drivermock["driver/mock, driver/drivertest"]
    androiddriver["androiddriver"]
    iosdriver["iosdriver"]
    iossimdriver["iossimdriver"]
    orchestra["orchestra (selector resolution, scroll, obscurement, waits)"]
    script["script (parser + expression engine)"]
    runner["runner (step handlers)"]
    scenario["scenario (script and suite sequencing)"]
    device["device (probe + factory)"]
    session["session (Open: identity, lease, driver)"]

    mcp["mcp (MCP server)"]
    cli["cli (atomyx binary)"]
    sidecar["sidecar (JSON-RPC bridge)"]

    flock --> lease
    diag --> lease
    clock --> lease
    wire --> driver
    transport --> driver
    driver --> androiddriver
    driver --> iosdriver
    driver --> iossimdriver
    process --> androiddriver
    process --> iosdriver
    process --> iossimdriver
    driver --> drivermock
    driver --> orchestra
    drivermock --> orchestra
    orchestra --> script
    script --> runner
    runner --> scenario
    driver --> device
    androiddriver --> device
    iosdriver --> device
    iossimdriver --> device
    orchestra --> device
    device --> session
    lease --> session
    session --> cli
    session --> mcp

    orchestra --> mcp
    runner --> mcp
    device --> mcp
    runner --> cli
    device --> cli
    scenario --> cli
    device --> sidecar
    runner --> sidecar
```

Reading the diagram: an arrow means "the arrow's tail may depend on
its head." `mcp`, `cli`, and `sidecar` are parallel consumers of the
same `orchestra` and `runner` contracts, and none of them wraps another.

## The Driver port

`core/internal/driver/driver.go` defines `Driver`, the platform-neutral
device control surface every concrete adapter implements. Port names
are platform-neutral by design: `Platform()` returns `"ios"` or
`"android"`, not a package name. `AppID` replaces a platform-specific
concept like a bundle identifier or a package name at the port
boundary.

A `Driver` implementation reports static feature support through
`Capabilities()` (`core/internal/driver/types.go`), which callers
above the port must check before invoking a conditionally supported
method:

- `CanEraseText`: native backward erase from the focused field.
- `CanHideKeyboard`: native keyboard dismissal.
- `CanWaitForIdle`: a native idle-detection primitive. `orchestra`
  falls back to tree-diff polling when absent.
- `CanMultiPointer`: multi-pointer gesture dispatch (pinch, rotate,
  multi-finger custom paths).
- `CanPressure`: per-waypoint pressure values inside a gesture.

Rule of thumb stated directly in the port's doc comment: if a method
can be implemented by composing other `Driver` methods, it does not
belong on the interface. Selector resolution, scroll-into-view,
obscurement detection, and retry all live one layer up, in
`orchestra`.

## The Orchestra layer

`core/internal/orchestra` composes `Driver` calls into the operations
every consumer (script engine, MCP tools, CLI) actually wants:
selector resolution and priority broadening, scroll-into-view via the
scroll controller (`core/internal/orchestra/scroll_controller.go`),
obscurement detection, and the wait primitives in
`core/internal/orchestra/wait.go`. The tree-query helpers live in
`core/internal/orchestra/finder.go`. `Find` and `FindOne` run a
compiled selector filter against a hierarchy and return matching
cursors.

### Selector priority broadening

`core/internal/orchestra/priority_broadening.go` defines
`CompileSelector`, which turns a `Selector` (the fields an agent or a
script step supplies) into an `ElementFilter`. Content fields are
tried in a fixed priority order, most specific to most fuzzy:

```
ID > Label > Text > Value > Hint
```

The first field that produces a non-empty match wins, so broadening
stops there. When the caller supplies `Text` but not `Label`, a
`Label` match against the same pattern is tried right after `Text`,
because a cross-platform text query against an element that only
carries an accessibility label still needs to resolve. `Role`,
`Enabled`, `Clickable`, and `Focused` are not part of the priority
chain: every candidate filter AND-s them in as constraints. `Nth`
skips ranking entirely and selects positionally from the winning
filter's result list.

This is the mechanism that lets an agent write `{text: "Login"}`
without knowing whether Android exposes that string through
`contentDesc` or iOS through `label`: both adapters normalize onto
the same canonical attribute keys at the `Driver` boundary, and
`CompileSelector` tries whichever fields the caller supplied in
priority order.

## Driver implementers

Four packages implement `Driver`, all verified against the same
contract suite in `core/internal/driver/drivertest`:

- **Mock** (`core/internal/driver/mock`): an in-memory implementation
  for tests. Production code must never import it. Only test files
  and the contract suite depend on it.
- **Android** (`core/internal/androiddriver`): talks to the Android
  agent's HTTP control server over `127.0.0.1:8765`, reached through
  an `adb forward` tunnel the driver manages.
- **iOS device and simulator via XCUITest**
  (`core/internal/iosdriver`): talks to the Swift XCUITest runner
  over a line-delimited JSON TCP protocol on `127.0.0.1:22087`.
  Connecting to a physical device additionally runs an `iproxy` USB
  tunnel to map that port.
- **iOS simulator via direct HID** (`core/internal/iossimdriver`):
  dispatches touch gestures through a WebSocket connection to a
  helper process wrapping Apple's private simulator HID client,
  bypassing XCUITest for the simulator-only fast path.

Each implementer's own package doc comment describes its transport in
more detail. The contract suite in `drivertest` is what keeps all
four honest against the same `Driver` invariants as the port
evolves.

## Script engine and step handlers

`core/internal/script` owns the test-script schema, the YAML parser,
and the expression engine (`core/internal/script/expression`) that
evaluates the boolean and arithmetic expressions used by
`assertExpr`, branch and handle conditions, and extract and capture
patterns. `core/internal/runner` defines the command execution
contract (`CommandContext`, `CommandResult`, `CommandDefinition`) and
the `ScriptRunner` that dispatches a parsed script's steps against
that contract, one step at a time, against an `Orchestra` instance,
failing fast on the first step that does not succeed.

`core/internal/scenario` sequences groups of scripts (a scenario) and
ordered sets of scripts or scenarios (a suite), giving each entry its
own fresh `ScriptRunner` so precondition, guard, and per-run data
isolation stay entirely inside `runner`.

## The MCP surface

`core/internal/mcp` builds the Atomyx MCP server: the stdio-based
process an AI agent connects to for tool-driven mobile test
orchestration. Every tool handler is a thin wrapper that validates
its arguments against a JSON Schema, calls one or more `Orchestra` (or,
for run and script tools, `RunStore` or `ScriptRunner`) methods, and
returns a JSON result. See [`tools.md`](./tools.md) for the full
tool-by-tool contract. MCP is an adapter over the same core the CLI
uses, and it never sits between the CLI and `Orchestra`.

## The CLI

`core/internal/cli` implements the `atomyx` binary: argument parsing,
command routing, and the driver module's commands (`run`, `suite`,
`list-devices`, `doctor`, `version`, `help`) plus the skills module's
commands (`init`, `update-skills`) that install the bundled skill and
agent markdown files from `core/internal/skills` into a consumer
project.

## Device lease and identity

`core/internal/lease` gives each physical device at most one driving
Atomyx process. A lease is an exclusive `flock` on a per-device lock
file in a per-user runtime directory. The kernel owns liveness: the
flock sits on an open descriptor for the process lifetime and
disappears when the process exits for any reason, so a crashed holder
never leaves a stale lock.

Contract:

- **Key.** The first 16 hex characters of the SHA-256 of
  `<platform>:<hardwareId>`. The files are `<key>.lock` and
  `<key>.sock`.
- **Runtime directory.** On Linux, `$XDG_RUNTIME_DIR/atomyx`. Elsewhere,
  a `run` subdirectory of the user cache directory. The per-user
  fallback `/tmp/atomyx-<uid>` replaces that choice when it is unset,
  when a socket path under it would exceed 104 bytes, or when its
  filesystem is not local (on macOS, not mounted with `MNT_LOCAL`). The
  directory is created with mode 0700, must be a real directory (not a
  symlink) owned by the current user, and otherwise the error is an
  `UnsafeDirError`.
- **Conflict.** `Acquire` retries a contended flock for about 200
  milliseconds, then returns a `HeldError` carrying `DEVICE_HELD` and
  the holder's display metadata. `DEVICE_BUSY` stays reserved for a
  foreign, non-Atomyx automation session, and the two codes never mean
  the same thing. `DEVICE_RELEASED_HELD` (an agent session that gave up
  its device and finds it held) and `RUN_EXPIRED` (a tool call on a run
  closed by the open-run idle timeout) are the other two codes the
  lease design defines, built by `diag.NewDeviceReleasedHeld` and
  `diag.NewRunExpired`.
- **Replacement check.** After the flock succeeds, the descriptor is
  compared with a fresh `stat` of the path, and a mismatch restarts the
  acquire a bounded number of times. `Lease.Recheck` repeats the
  comparison for a long-lived holder, and `Lease.Watch` runs it every
  `InodeRecheckPeriod` (5 seconds). A failed re-acquire returns an error
  wrapping `ErrLost`.
- **Lock file.** It is never unlinked. It carries the holder's
  `{pid, command, role, runId, socket, startedAt, idleSince}` for
  display only, and nothing reads that metadata to make a control
  decision.
- **Probe.** `Manager.Probe` takes the same real flock and releases it
  immediately. It never creates the lock file and never writes holder
  metadata.

Canonical identity lives in `core/internal/device/identity.go`.
`IdentityResolver.Resolve` runs before any agent connect, through
queries with no side effect on the device:

- Android: `ro.serialno` read through adb, so one phone reached over USB
  and over Wi-Fi maps to one lease. When `ro.serialno` is empty or a
  known generic value, the identity is the AVD name from
  `ro.boot.qemu.avd_name` joined with the adb serial. An unauthorized,
  offline or unavailable device returns a `DeviceStateError` instead of
  an empty serial.
- iOS: the UDID from the descriptor.

`lease` may depend only on `diag` and the common leaves. It does not
import `device`, so the caller resolves the identity first and passes
the key down.

`session` reaches the lease manager through its narrow `LeaseAcquirer`
interface, which `*lease.Manager` satisfies, so `mcp` does not import
`lease`. A host test passes a `sessiontest.Leases` from
`core/internal/session/sessiontest` instead of a manager. Only test
files import that package, and `core/scripts/check-sessiontest-imports.sh`
fails the build when a production file does.

## Session ownership

`core/internal/session` is the one path by which a process takes a
device. It has two phases. `session.Prepare(ctx, deps, desc, buildMs)`
runs first and takes no lease: it does the slow work a device needs,
such as building the iOS runner, under the fixed runner build bound of
300 seconds, and fails with `RUNNER_BUILD_TIMEOUT` when that elapses.
`session.Open(ctx, deps, req)` then runs under the acquisition budget
and does these steps in this order. `session.Acquire` runs both phases
for a host that does not need to split them.

1. Resolves the canonical identity through `device.IdentityResolver`.
   An identity failure returns a coded error before any lease is taken:
   an unauthorized, offline or unavailable Android device is
   `AGENT_UNREACHABLE` with a remedy for that state, and a device that
   exposes no usable identity is `DEVICE_NOT_FOUND`.
2. Acquires the lease for that identity. A conflict is `DEVICE_HELD`,
   or `DEVICE_RELEASED_HELD` when the request is a `Reacquire`, and in
   both cases no driver is built.
3. For an iOS target, ends an orphaned runner, and the `iproxy` tunnel of
   a physical device, that a crashed earlier holder left behind, through
   `device.RunnerReaper`. This runs only after the
   lease is held, because that is what proves the previous holder is
   gone. A failure to end the orphan is logged and does not block the
   open.
4. Builds the driver through `device.Factory`, with the device tagged on
   the context through `logging.WithDevice` so every log line from the
   driver carries `deviceId` and `hardwareId`.
5. Offers the previous driver's launched-app state to the new driver.
   The iOS drivers apply it only when the runner they reached reports the
   same launch id as the one that launched the app. When the new driver
   could not take it, the session reports `Relaunched`, and the MCP
   server tells the agent that the app was relaunched on a new runner.

The returned `Session` owns the lease, the driver and a goroutine that
runs `Lease.Watch`. A replaced lock file that cannot be re-acquired
closes `Session.Lost()`. `Session.Close` stops the watcher, disconnects
the driver within the shared shutdown grace period, which also stops a
runner the factory launched, and then releases the lease. The lock-file
metadata follows the holder through `SetRunning` and `SetIdle`, so a
second process that finds the device held can report why.

The CLI commands `run`, `suite` and `doctor` go through `session.Acquire`,
and the MCP server's device selection and re-acquire go through
`session.Prepare` and `session.Open`. A nil `Deps.Leases` disables
exclusion for a single-process host such as a test, and every production
composition root sets it.

`session` may depend on `device`, `lease`, `diag`, `orchestra`, `driver`,
`shutdown` and the common leaves. It never imports `mcp`, `cli` or
`inspector`.

The two hosts own a session differently. A CLI command holds it for the
length of the command and cancels its run when `Session.Lost()` closes.
The MCP server holds it through `DeviceSession`, which releases the
device after an idle timeout, takes it again on the next call that needs
it, and runs acquisition outside the tool call budget. Every transition
of its held session happens under one lock, a failed selection keeps the
working device, and the health report carries the holder state. The idle
timeouts and the acquisition budget come from the `session` block of the project
config (`mcpIdleTimeoutMs`, `openRunIdleTimeoutMs`,
`deviceAcquireTimeoutMs`), with `ATOMYX_MCP_IDLE_TIMEOUT_MS` and
`ATOMYX_DEVICE_ACQUIRE_TIMEOUT_MS` overriding the first and last. The
runner build bound has no key, so it is fixed. Both binaries read the
project config from the working directory.
`cmd/internal/sessionwire` resolves them. The holder state machine is
documented in [`tools.md`](./tools.md).

## Run folder contract

`core/internal/runfolder` owns the on-disk shape of a run. Consumers
ask it for paths and types and never spell the layout themselves.

```
<root>/runs/v1/<runId>/
  meta.json
  steps.jsonl
  log.jsonl
  console.log
  artifacts/
  report/
  interventions.jsonl
  run.lock
```

Contract:

- **Meta.** `meta.json` carries `schemaVersion`, `runId`, `kind`
  (`script`, `suite`, or `mcp`), optional script and device fields,
  `startedAt` and `completedAt` as epoch milliseconds, `result`,
  `outcome`, `failedAtStep`, `totalSteps`, and an optional `mcp` block
  for the fields only an MCP run has.
- **Result and outcome.** `result` is `passed`, `failed`, or
  `cancelled`. `outcome` refines it with `passed`, `failed`, `error`,
  `cancelled`, `deviceLost`, or `interrupted`, and each outcome belongs
  to exactly one result. `result` and `outcome` are written only
  together with `completedAt`. A meta with no `completedAt` is a run
  that never finished, and a reader reports it as result `cancelled`
  with outcome `interrupted`. A migrated legacy running record
  therefore carries no `completedAt`, no `result`, and no `outcome`.
- **Kind.** `script` is a CLI run of one script, `suite` is a CLI
  suite or scenario run, and `mcp` is a run driven through the MCP
  server. A `run_script` call inside an open MCP run writes into that
  run, and a bare `run_script` call is its own run of kind `mcp`.
- **Failed step.** `failedAtStep` is the zero-based index of the
  failed step, matching `ScriptResult.FailedAtStep` in the runner.
  Display code adds one.
- **Run id.** The schema puts no pattern on it, because a migrated run
  keeps its legacy id. A run written by `runfolder.Starter` gets
  `<yyyymmdd-hhmmss>-<label>-<4 hex>`. The time is UTC. The label is
  lowercased, every character outside a to z and 0 to 9 becomes a
  dash, repeated dashes collapse, edge dashes are dropped, and the
  result is cut to 24 characters, with `mcp` when nothing is left. The
  folder is made with an exclusive mkdir, and on a collision the four
  hex characters are drawn again up to 8 times before `Start` fails.
- **Write lifecycle.** `Start` writes `meta.json` with no
  `completedAt`, `result`, or `outcome`. `Finalize` takes the outcome,
  derives `result` from it, and replaces `meta.json` through a
  temporary file and rename. A second `Finalize` returns an error and
  changes nothing. `Close` without `Finalize` leaves the run to read
  as interrupted.
- **Live lock.** A live `Writer` holds an exclusive lock on `run.lock`
  inside the run folder, released by `Finalize` or `Close` and by the
  kernel when the process dies. `LockLiveness` reports a run as live
  when that lock is held, from any process, and `AnyLive` combines it
  with other liveness signals.
- **Log bound.** `log.jsonl` grows to at most 8 MiB of lines. The
  first line past the bound is dropped and one marker line
  `{"truncated":true,"limitBytes":N}` ends the file, after which
  further lines are dropped silently.
- **Evidence.** One `artifacts/` directory holds every screenshot and
  piece of failure evidence. An MCP `screenshot` taken with no run open
  goes to `LooseScreenshotsDir` under the root instead. The report lives in the sibling
  `report/` directory and links files as `../artifacts/<file>`.
- **Compatibility.** Readers accept `schemaVersion` values up to their
  own and ignore unknown fields. Writers never reshape a field within
  a major version.
- **Legacy mapping.** A flat legacy record status maps as `passed` to
  passed, `failed` to failed, `error` to failed with outcome `error`,
  `cancelled` to cancelled, and `running` to cancelled with outcome
  `interrupted`.
- **Schema.** `core/internal/runfolder/atomyx-run.schema.json` is
  generated by `core/cmd/gen-run-schema` and covered by the
  schema-freshness gate.

`runfolder` depends on the standard library and `core/internal/flock`,
so every consumer may import it. The arch-lint rules give it no
dependency on any other project package. The Go program in `core/scripts/check-runfolder-layout`
keeps the layout names out of every other package.

## Dependency direction and its enforcement

Dependency direction across every package under `core/internal/` is
declared in `core/.go-arch-lint.yml` and enforced locally by
`go-arch-lint check --project-path .`, part of `make -C core check`
(see [`development.md`](./development.md) for the full gate list). The
config lists every package as a named component and an explicit
`mayDependOn` edge list, and an import that is not on that list fails
the check.

The shape the config encodes:

- **Leaf components** (`clock`, `logging`, `version`, `diag`, `wire`)
  carry no business knowledge and may be depended on by nearly
  everything, directly mirroring the "leaf package" language in their
  own doc comments.
- **`transport`** may depend only on `wire` and `diag`.
- **`lease`** may depend only on `diag` and the common leaves.
- **`session`** may depend on `device`, `lease`, `orchestra`, `driver`,
  `diag` and `shutdown`. `session/sessiontest` may depend only on `lease`,
  and `mcp` may depend on it for tests. `cli` and `mcp` may depend on `session`, and it
  may not depend on either of them.
- **`driver`** may depend only on `wire` and `transport`. The three
  concrete implementers (`androiddriver`, `iosdriver`,
  `iossimdriver`) may depend on `driver`, `transport`, `process`, and
  the `drivertest` contract suite, but not on each other.
- **`orchestra`** may depend on `driver` and `drivermock` (for its own
  tests), never on a concrete implementer. This is what keeps
  `Orchestra` platform-neutral: it is compiled against the port, not
  against Android or iOS code.
- **`script`** depends on `driver` and `orchestra`.
- **`runner`** additionally depends on `script` and `expression`.
- **`scenario`** additionally depends on `runner`.
- **`device`** (discovery and construction) is the one package
  allowed to depend on `driver` and all three concrete implementers
  at once, because assembling a live `Driver` for whichever platform
  a caller selects is exactly its job.
- **`mcp`**, **`cli`**, and **`sidecar`** sit at the top: each may
  depend on `orchestra`, `script`, `runner`, `device`, and their own
  supporting leaves, but none of them may depend on each other. This
  is the enforcement mechanism behind "MCP, CLI, and the sidecar are
  parallel consumers, not stacked layers."
- **`conformance`** and **`cmd`** are declared `anyProjectDeps: true`:
  the differential test corpus and the binary entry points are
  allowed to import anything, because their job is exercising or
  wiring the rest of the module, not being depended upon.

A change that needs an edge the config does not grant is a signal to
reconsider the change's layer, not to add a blanket allowance.

## Design principles

These hold regardless of which layer a change touches:

- **Cross-platform by design.** Every capability an agent or a script
  step can invoke goes through the `Driver` port. Port and
  `Capabilities` field names are platform-neutral.
- **One tool per intent.** The MCP surface stays small on purpose;
  before adding a tool, confirm no existing tool can absorb the
  intent through a parameter.
- **No platform branching above the driver adapter.** `orchestra`,
  `script`, `runner`, and `mcp` never switch on `Platform()`. A
  behavior that genuinely differs per platform belongs inside the
  concrete `Driver` implementer, not in a conditional above it.
- **Selector-first, coordinates first-class.** Actions accept a
  `Selector` or raw `{x, y}` coordinates as an equally valid,
  non-hacky alternative, because not every element carries a stable
  selector.
- **Agent ergonomics over engine purity.** A tool name, error message,
  or default that makes the calling agent's life easier wins even
  when it pushes complexity into the tool layer that would otherwise
  "belong" lower in the stack.

## References

- [`repo-map.md`](./repo-map.md): where each package and binary lives
  on disk.
- [`development.md`](./development.md): build, test, and extension
  workflow, including running `go-arch-lint` locally.
- [`tools.md`](./tools.md): the full MCP tool contract.
