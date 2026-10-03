# Atomyx: Known pitfalls

Traps the team has already hit once. Read before touching the
corresponding area. For the high-level map see
[`repo-map.md`](./repo-map.md).

## Android control plane

- **Never cache `AccessibilityNodeInfo` references across tool calls.**
  They go stale. `UiTreeService`
  (`platforms/android-agent/app/src/main/java/dev/atomyx/agent/control/UiTreeService.kt`)
  is a metadata-only registry by design: it captures `bounds: Rect` and
  stable selectors at dump time, uses the cached bounds for gestures,
  and re-resolves a fresh node from the live tree for any action that
  needs one.
- **Declare `mutates` on every new agent route.** `Route.mutates` is
  abstract, so a route cannot compile without a choice. The router
  runs mutating routes one at a time. Mark a route false only when it
  reads state or must stay answerable during a held gesture, because
  a false route runs concurrently with whatever action is in flight.
  A new route goes into `ShippedRoutes`, and `RouterLockTest` fails
  until its path is classified as a read or runs behind the lock.
- **A mutating route that waits too long is refused, never delayed.**
  `Router` waits for its lock at most `MUTATION_LOCK_WAIT_MS` and then
  returns `429` with `retryable: true` without running the route.
  Never replace the bounded wait by an unbounded lock, because the host
  has already given up by then and the action would fire as a ghost.
- **Add a capability to `/ping` instead of changing its existing
  fields.** The host reads `capabilities.secureFields` to detect an
  agent that cannot mark secret fields, and an agent without it is
  reinstalled or refused. A new agent behavior the host depends on
  needs its own capability and the same stale-agent handling.
- **Never replace `serviceInfo` wholesale in `onServiceConnected`.**
  Augment it instead. Replacing it with a fresh
  `AccessibilityServiceInfo()` clobbers the capability and flag values
  the manifest's accessibility config declared and causes
  `service.windows` to return empty until the service is re-bound. See
  `platforms/android-agent/app/src/main/java/dev/atomyx/agent/service/AtomyxAccessibilityService.kt`.
- **The tap pipeline auto-dismisses the keyboard when it obscures the
  target.** Orchestra's `Tap(selector)` pipeline
  (`core/internal/orchestra/orchestra.go`) checks the session flag
  `maybeKeyboardOpen` together with the obscurement result's role.
  When both indicate a keyboard is blocking the target, it dismisses
  the keyboard before retrying instead of dispatching a tap that would
  land on the IME.
- **Never call `typeViaKeyboard` without accounting for IME layout
  switches.** When focus moves from a numeric field to a text field,
  the keyboard re-renders. `KeyboardTyper.typeViaKeyboard`
  (`platforms/android-agent/app/src/main/java/dev/atomyx/agent/control/KeyboardTyper.kt`)
  polls internally before per-key dispatch and falls through to an
  on-screen-key scan for custom in-app keypads (banking apps, OTP
  entry widgets, most common on Flutter).
- **`UiTreeService.dumpCompact` keeps any element with a stable
  signal** (clickable flag, text, contentDesc, or resourceId), not
  only clickable-or-labeled elements. Flutter elements frequently
  carry only a resourceId. `dumpCompact` is an internal helper
  consumed by the keyboard and clear-text fallbacks
  (`platforms/android-agent/app/src/main/java/dev/atomyx/agent/control/InputDispatcher.kt`,
  `platforms/android-agent/app/src/main/java/dev/atomyx/agent/control/KeyboardTyper.kt`,
  and the strategies under
  `platforms/android-agent/app/src/main/java/dev/atomyx/agent/control/clear/`),
  not a wire route, so tightening its filter breaks those fallbacks
  silently rather than an API contract.
- **`clickable` is a ranking signal, never a hard filter.** Flutter,
  Compose, and React Native dispatch gestures in-engine via a gesture
  detector without setting the accessibility clickable bit, so a
  selector match must not require it. `CompileSelector` in
  `core/internal/orchestra/priority_broadening.go` only applies
  `IsClickable()` when the caller explicitly asks for
  `Selector.Clickable == true`. Otherwise `rankCandidates` uses
  clickable (plus focused) only to break ties among multiple matches.
- **Never run gesture commands on the accessibility-service main
  thread.** `AccessibilityService.dispatchGesture(g, callback,
  handler=null)` delivers its `GestureResultCallback` on the service's
  main thread. Calling `GestureRunner.dispatch`
  (`platforms/android-agent/app/src/main/java/dev/atomyx/agent/control/GestureRunner.kt`)
  from main would block main waiting on a latch that only the main
  thread can fire, indistinguishable from a 30s hang. NanoHTTPD's
  worker threads are the supported entry point. A new code path that
  dispatches gestures from a non-route context must route through a
  background executor first.
- **Two-tier capability gate for `DispatchGesture`.** The Go adapter
  (`core/internal/androiddriver/driver.go`) rejects a multi-pointer or
  pressure-carrying gesture before it ever leaves the host, and the
  agent's `GestureRunner.validatePointers` re-validates everything on
  arrival. Either tier alone is insufficient, because a bypassed host
  (direct HTTP poke) or a bypassed agent (older APK build) would leak
  an unsafe gesture. Removing a check from one side is a regression
  even when the other still catches it.
- **`GestureDescription.StrokeDescription` requires `duration > 0`.**
  `GestureRunner.validatePointers` enforces monotonic-non-decreasing
  waypoint offsets at parse time, and a zero-duration tap survives
  because `MIN_PRESS_MS` (50ms) floors every issued stroke. A new
  stroke-building path must apply the same floor, or the platform
  rejects the gesture with an uncaught `IllegalArgumentException`
  rather than a structured rejection.
- **`canPressure` is permanently `false` on Android.** The
  accessibility gesture surface has no per-touch pressure API on any
  current Android level. Do not add a "maybe supported on device X"
  flip: the entire backend is pressure-less, and the host validator
  relies on this being a hard no. The single place to flip if the
  platform ever ships a pressure-carrying `StrokeDescription`
  constructor is `GestureRunner.GestureCapabilities.DEFAULT`.
- **Long-press-then-drag must flow through the multi-phase
  dispatcher, not a single `GestureDescription`.** A zero-length
  segment inside one `GestureDescription` still emits periodic
  same-coordinate `ACTION_MOVE` events (the platform interpolates at
  roughly 10ms inside every stroke's active window regardless of path
  length). A recognizer that rejects on the first move event, such as
  Flutter's `DelayedMultiDragGestureRecognizer` backing
  `ReorderableListView`, refuses that shape. `dispatchMultiPhase`
  instead issues a minimal `MIN_PRESS_MS` stroke for a hold segment,
  then sleeps the authored remainder at the Kotlin level between
  dispatch calls, producing no platform events during the hold.
- **Pointer coordinates are physical pixels, while Flutter lays out
  in logical pixels.** A Flutter app's own coordinate space is scaled
  by its `devicePixelRatio`. A script or tool computing a tap target
  from Flutter layout data must multiply by that ratio before
  dispatching through `Tap` or `DispatchGesture`.
- **A hold must exceed roughly 500ms to trigger a Flutter
  long-press.** Flutter's `LongPressGestureRecognizer` uses its own
  timer independent of the platform's, so an authored hold shorter
  than that risks resolving as a tap instead.
- **`adb forward` can drop between calls.** A USB disconnect, `adb
  kill-server`, or a device reboot drops every forward.
  `AgentLauncher.EnsureRunning`
  (`core/internal/androiddriver/agent_launcher.go`) re-issues the
  forward unconditionally rather than assuming a previously connected
  session is still reachable, and a caller building a new code path
  against the agent should do the same.
- **A host-side forward is a shared resource, not a per-driver one.**
  The local port `AndroidDriver.Connect`
  (`core/internal/androiddriver/driver.go`) forwards to the device is
  a real listening socket on the workstation. Never hard-code a host
  port for a new Android driver: set `Options.AllocateHostPort` so adb
  picks a free one per device, and supply `Options.NewTransport` so the
  client is built after the port is known. `Connect` reuses a forward
  that already maps this serial to the device port, and
  `Disconnect` removes only a forward this `Connect` created. With a
  fixed `HostPort`, a port already forwarded to a different device
  fails with `HOST_PORT_IN_USE`, not `DEVICE_BUSY`, which is reserved
  for a foreign automation session.

## Device lease

- **Acquire the lease before any agent connect.** Resolve the device's
  canonical identity first (`device.IdentityResolver.Resolve`), take the
  lease, and only then forward ports or launch a runner. Connecting
  first disturbs a live holder's forward or runner.
- **Key on the hardware identity, never on the transport string.** The
  Wi-Fi serial `<ip>:<port>` and the USB serial of the same phone must
  map to one lease, which is why identity reads `ro.serialno`.
- **Never unlink a lock file.** It is created once and reused. Replacing
  it is exactly what the descriptor-versus-path comparison detects, so
  removing and recreating it defeats that check. Only a socket may be
  unlinked, and only by the process holding its flock.
- **Never open a lease file with a raw `syscall.Open` or `unix.Open`.**
  Use `os.OpenFile` so close-on-exec applies, because a leaked lease
  descriptor in a spawned child keeps the device held after the owner
  exits. `core/scripts/check-no-raw-syscall-open.sh` fails the build on
  a raw open outside `core/internal/lease`.
- **Take a file lock through `core/internal/flock` only.** The package
  is a leaf, so it must not import another project package. It never
  switches the descriptor to blocking mode and it retries on a signal.
  A lock lives on an open file description, so open the file with
  `os.OpenFile` and keep it open for as long as the lock must hold.
- **Never conflate `DEVICE_HELD` with `DEVICE_BUSY`.** `DEVICE_HELD`
  means another Atomyx process holds the lease. `DEVICE_BUSY` is
  reserved for a foreign, non-Atomyx automation session found driving
  the device. The remedies differ, so a handler that returns the wrong
  one points the operator at the wrong fix.
- **A probe never writes.** `atomyx devices` and `doctor --bundle` use
  `Manager.Probe`, which takes and releases the real flock but never
  creates the lock file or writes holder metadata. Only the process that
  keeps the flock for its session owns that write.
- **Lock file contents are display only.** Liveness comes from the
  flock, never from the pid or command in the file, because a dead
  holder's metadata can linger until the next acquirer overwrites it.

## Session ownership

- **Reap a recorded runner only after the lease is held.** The
  provisioner reaps while it launches, which runs after `session.Open`
  took the lease. A held lease proves no other session drives the
  device. A free lease does not prove the record's owner is gone,
  because a runner outlives the session that used it. Reaping before the
  lease, or without it, can signal the runner of a live holder.
- **Adopt a recorded runner only when every condition holds.** The
  record must name this device, its owner must be this process or gone,
  the recorded `xcodebuild` process must still be the recorded one, the
  runner must answer with the recorded launch id, and the record's build
  product must match the product on disk. Dropping the build check
  adopts a runner that runs old Swift code. Dropping the identity check
  adopts whatever now answers on a recycled port. Only one runner fits
  per simulator, so an adopted runner that fails the post-connect probe
  is ended before a fresh one is launched.
- **An adopted runner has no child process.** Its loss is found by
  polling the recorded pid and start time, so a change to that watcher
  must keep comparing the start time, because a recycled pid is a
  different process.
- **Never signal a recorded process group without proving it is the
  recorded process.** The pid, start time, user, command (`xcodebuild`
  for the runner, `iproxy` for the tunnel of a physical device) and
  process group must all match, the host must not have rebooted, and the
  group must be greater than 1 and not the caller's own. The runner
  record names the tunnel through `tunnelPid` and `tunnelStartTime`, and
  a tunnel that fails any check is left running. A pid can be
  reused after the original process exits.
- **Read process identity from the kernel, not from `ps` text.** The
  start time, user, command and process group come from one
  `process.Inspector` query, so they describe the same process. A
  parsed `ps` line is zone and locale dependent.
- **A simulator runner outlives its `xcodebuild` group.** The runner app
  runs under the simulator's launchd. After signaling the group, confirm
  the recorded port no longer answers with the recorded runner id, and
  fall back to `simctl terminate` when it does.
- **Take the lease before `EnsureIOSAgent`.** Two launches for one device
  share `build/run-<udid>`, so the lease is what serializes them.
- **Build the runner in `session.Prepare`, before the lease, and never
  under `DeviceSession.acquireMu`.** The build is the one slow step that
  needs no ownership of the device. Run under the lease or the lock, it
  blocks other processes from the device, and in the MCP server it also
  blocks the idle timer and the lost-lease watcher for up to the build
  bound.
- **Run the provisioner's `kill` once.** It stops the runner and releases
  the reserved port. A second call must not release a port a later
  launch has since reserved.
- **Never count device acquisition against an MCP tool budget.** A first
  iOS launch compiles the runner and a re-acquire waits for a lease, so
  both can run far longer than the 30 second tool call budget.
  `DeviceSession.Select` and `DeviceSession.Ensure` pause the call's
  budget with `pauseToolBudget`, bound the build by the fixed
  `session.Timeouts.RunnerBuildMs` (300 seconds, `RUNNER_BUILD_TIMEOUT`),
  and bound the acquisition by `session.deviceAcquireTimeoutMs` (120
  seconds by default, `TOOL_TIMEOUT`) instead. The build never spends
  the acquisition budget.
- **A tool that only observes registers through `addObserveOnlyTool`.**
  The call middleware reads that declaration, not a list of tool names,
  so a polling tool such as `check_health` neither marks the device
  running nor restarts the idle timer. `DeviceSession.CheckHealth` never
  calls `Ensure`, because a health check must not take a released device
  or wait for a build.
- **Failure capture is bounded and skipped on device loss.**
  `runner.FailureBundle` stops at `runner.FailureCaptureBoundMs` (5000
  ms) on the injected clock, so a hung screenshot cannot hold a failed
  run open. A step failure that the liveness probe classifies as
  `RunAbortDeviceLost` captures nothing, because the screenshot of an
  unreachable device cannot succeed. A test that exercises a capture
  with real files injects `clock.ManualClock` or the system clock,
  because a `FakeClock` sleep returns at once and would fire the bound
  immediately.
- **A lost device is signaled by a typed cause, never by a string.**
  Every cause `session.Session.LostErr` reports wraps
  `session.ErrDeviceLost`, so a call that holds only a cancellation
  cause tells a lost device from a user stop with `errors.Is`. The MCP
  server cancels running scripts with that cause before it closes the
  open run, and `scriptOutcome` checks the cause before it checks for a
  plain cancellation, because a loss that cancels the call context
  would otherwise record `cancelled`. The runner-exit watcher lives in
  the provisioner and the session, never in a tool handler or a CLI
  command, and it ends the remaining runner and tunnel itself through
  the one kill function, which runs once however many paths reach it.
- **Give a test lease helper to `session/sessiontest`, never to a
  production package.** Only `_test.go` files import
  `core/internal/session/sessiontest`, which
  `core/scripts/check-test-support-imports.sh` enforces, so `mcp` keeps no
  direct edge to `lease` in production.
- **Never fetch from the device for a viewer.** A viewer reads
  `Session.Latest()` and `Session.Subscribe`, which return what the
  runner, a tool call or failure capture already fetched. A fetch on
  behalf of a viewer while a run executes interleaves with the run's own
  device calls and slows it.
- **Take the activity gate around every call that drives the device.** A
  fresh capture runs only while no tool call is inside the gate, and
  `FreshTree` returns `HOLDER_BUSY` instead of waiting when one is. A
  caller of `FreshTree` retries later and never loops on it. A host
  that drives the device without entering it lets a capture interleave
  with the call and pair a tree with a screenshot from different screens.
  The MCP middleware enters it, so a handler never does, and a new tool
  is covered by registering it as it is.
- **Masking lives in the viewer path, so read trees for display only
  through `Latest()`.** The hub stores the real tree for the Orchestra's
  sake and masks it on read. Code that reads the stored tree some other
  way would show secrets. Treat every slice and map in a `Snapshot` as
  read-only, because the masked tree shares its unmasked attribute maps
  with the stored one.
- **A session factory must implement `device.WrappingFactory`.** The type
  of `session.Deps.Factory`, `mcp.ServerDeps.Factory` and `cli.Deps.Factory`
  requires it. A test factory gets it from `sessiontest.Wrapping`, which
  wraps `Built.Driver` and rebuilds the Orchestra over the wrapped driver
  on the system clock, so the runner and the tool handlers publish
  captures like production. A test whose Orchestra needs another clock
  passes a builder to `sessiontest.WrappingWith`.
- **Drive timers from the injected timer clock.** The acquisition budget,
  the tool call budget, and the idle timers use `ServerDeps.TimerClock`,
  not the orchestra clock. A test clock whose sleeps return at once would
  otherwise expire every timer immediately.
- **A cancellation panic in a command handler is not a bug.** A handler
  unwinds from an interrupted wait by panicking with the context error.
  The runner recovers it as an interrupted step without an error log, and
  only a panic while the run's context is still live is logged.

## Run folder layout

- **Never spell a layout name outside `core/internal/runfolder`.** The
  names `runs`, `v1`, `artifacts`, `report`, and `screenshots` are the
  on-disk contract of a run folder. Call `runfolder.RunDir`,
  `ArtifactsDir`, `ReportDir`, or `LooseScreenshotsDir` instead.
  The Go program in `core/scripts/check-runfolder-layout` parses
  each production file and flags a string literal, interpreted or
  raw, that equals `runs`, `artifacts`, or `screenshots`, that has
  one of the five names as a whole segment of a slashed path such as
  `"runs/v1"` or `screenshots/%s`, or that is exactly `v1` or
  `report` as an argument of a `filepath`, `path`, or `os` call.
  Struct tags, import paths, prose, URLs, and format identifiers such
  as `atomyx/v1` are not flagged. The gate has no allowlist, so a
  new spelling outside `runfolder` fails it.
- **A new field in `meta.json` needs a schema regeneration.** Change
  the `runfolder` types, then run `make -C core gen-schema`. The
  schema-freshness gate fails on a stale `atomyx-run.schema.json`.
- **A reshaped field breaks external readers.** Readers of
  `meta.json` ignore unknown fields, so adding a field is safe and
  renaming or retyping one is not. Treat that as a breaking change
  that needs an ADR. Studio keeps its own run root and does not read
  this folder.
- **Hold `run.lock` for the whole life of a run.** The lock marks the
  run live to every process, so close it only through `Finalize` or
  `Close`, and never unlink it. A probe through `LockLiveness` takes
  and drops the real lock but never creates the file. A run with no
  `completedAt` and no lock reads as interrupted, so a writer that
  skips `Finalize` is reported as interrupted even on a clean exit.
- **Create and lock `run.lock` under a temporary name, then rename it.**
  `Starter.Start` creates `run.lock.<random>.tmp` inside the run folder,
  takes the flock, and renames it to `run.lock`, so any `run.lock` a probe
  can open is already locked. Creating `run.lock` first and locking it
  afterwards leaves a gap where a probe takes the lock and `Start` fails,
  or finds an unlocked file and reports the starting run as not live. A
  probe treats a missing `run.lock` as not live. `meta.json` is written
  only after the lock is in place.
- **Finalize once, and only through `Finalize`.** `meta.json` is
  replaced through a temporary file and a rename, so never write it in
  place. `Finalize` fixes the verdict from the outcome, and a second
  call returns an error. Call it after the last `AppendLog`, because
  it closes the log.
- **Take `.migrate.lock` before touching a flat run record.** Run
  migration holds `<root>/runs/.migrate.lock` exclusively for its whole
  pass, and anything that deletes or edits a run record, whether the
  flat file or the run folder, holds it shared first. `RunCatalog.Delete`
  and `RunCatalog.SetSummary` do, and they refuse with
  `ErrMigrationInProgress` instead of waiting. The migration itself
  waits for the lock and announces it through its `onWait` callback. A new code path that
  writes or removes run records outside the catalog must take
  `runfolder.ShareMigration` too, or a migration can lose its edit or
  bring a deleted run back.
- **Make every migration step safe to repeat.** The folder is written
  and read back before the original is deleted, in that order, so a crash
  between the two leaves a verified folder and the original. The next
  pass recognizes the folder by its `importedFrom` marker, even after
  an edit, deletes only the original, and never writes a second copy.
  Never delete an original before its folder is verified, never drop
  the marker from an imported folder, and never reuse an id that holds
  a folder from another source. A read-back failure is reported as
  damaged and must not stop the pass.
  Scratch folders named `.import-*` under `runs/v1` are interrupted
  work and are not runs.

## Local HTTP surfaces

- **Never bind a non-loopback address.** A listener reachable from the
  network hands device control to other machines. Listen on a loopback
  literal such as `127.0.0.1:0`. The Go program in
  `core/scripts/check-listen-loopback` fails the build on an empty
  host, `0.0.0.0`, `[::]`, any other host, a computed address it cannot
  resolve, and the `ListenTCP` family. A new listener passes the gate
  by spelling its address as a literal or a constant in the same file.
- **Never let the inspector take the built driver or the Orchestra.**
  It reaches the device only through `session.Viewer` and
  `session.Controller`, because anything else runs device operations
  outside the activity gate. `core/scripts/check-inspector-boundary.sh`
  enforces it.
- **Never fetch for a viewer while a run executes.** The inspector
  asks the device for a capture only through `FreshTree`, which the
  activity gate refuses with `HOLDER_BUSY` while a tool call runs. Do
  not add a route that captures through another path.
- **Never count a read as inspector activity.** Only a route declared
  `Operator` resets the idle timer of `inspector.Host`, so polling the
  session, tree, screenshot or event routes never keeps the device. A
  new route that acts on behalf of the operator sets `Operator`, and a
  route that only reads what the session holds leaves it false.
- **Never re-acquire the device without the operator.** After an idle
  release, only `POST /api/v1/reconnect` takes it again, because an
  automatic re-acquire would let an open page hold the device forever.
- **Never accept the token anywhere but the `X-Atomyx-Token` header.**
  A token in a URL path or query lands in history, logs and referrers.
  Only `GET /` is exempt from the token, because it delivers the page
  that performs the exchange.

## Attach socket

- **Never treat a screenshot from the attach socket as masked.**
  `observe.screenshot.latest` and `observe.tree.fresh` return the
  device image exactly as captured, so a secure field shows on screen.
  Only the tree is masked. The protection is that the socket has mode
  0600 inside a 0700 directory and accepts only peers with the same uid
  as the holder. Never widen either, and never expose the socket
  through a port or a proxy.
- **Never push an image in a notice.** Notices carry a revision and an
  epoch, and the viewer pulls the image, so a slow viewer reads only
  the newest capture and never queues images.
- **Never block on a viewer.** Code that answers a request or forwards a
  notice only enqueues a frame. A full queue or a missed write deadline
  disconnects that viewer. A new code path that writes to a viewer
  socket directly would let one stalled viewer stall the holder.
- **Never change the process umask to protect the socket.** The socket
  gets mode 0600 by an explicit chmod right after bind, and the runtime
  directory is 0700.

## Holder socket lifecycle

- **Bind the holder socket only after the driver is built.** A failed
  build must leave no listener and no file, and the binding code runs in
  `session.Open` after `factory.Build` succeeds.
- **Remove a stale socket only while holding the lease.** The flock is
  what proves the previous holder is gone, so any file at the socket path
  then belongs to a dead process. Without the lease, removing the file
  could unbind a live holder.
- **Unlink the socket only while the path still names the file this
  session bound.** After a lost lease another holder may have bound the
  same path, and a blind remove would take that holder's socket away.
  `Session.Close` and a lost session both go through the same check.
- **Stop the attach server before the driver disconnects and the lease
  is released.** A viewer must see its connection end before the device
  is free for another process, and a request in flight must not reach a
  driver that is being torn down. Keep the stop inside the shared
  shutdown grace period together with the disconnect.
- **Keep the fresh-capture state check inside the activity gate.**
  `observe.tree.fresh` admits a viewer only when the holder state is
  `idle`, and the check runs after the gate is taken. Checking the state
  before the gate leaves a window in which a tool call starts between the
  check and the capture. A host that never calls `SetIdle`, such as a CLI
  command, refuses every fresh capture, which is the intended result.
- **Clear the intervention recorder before the run folder is
  finalized.** A viewer that detaches after `Finalize` would otherwise
  append to a closed writer and log a failure. A script that owns its own
  run must not replace the recorder of an open run, and must clear only
  its own.
- **Keep socket paths under 104 bytes in tests.** A longer path fails the
  bind, which the session logs and survives by serving no socket, so a
  test that needs a socket places its lease root under a short directory
  such as one made by `os.MkdirTemp("", "a")`.

## Tool layer (cross-platform)

- **Never branch on the session's platform inside a tool handler.**
  Files under `core/internal/mcp` read `device.Platform` off the
  active session for bookkeeping
  (`core/internal/mcp/device_session.go`), but no tool handler
  switches on it to change behavior. If behavior must differ
  per platform, it belongs in the driver adapter
  (`core/internal/androiddriver` or `core/internal/iosdriver`), not in
  the tool layer.
- **Never assume a device-side HTTP server exists.** That is
  Android-specific, because iOS talks TCP to an XCUITest process.
  Design a new
  tool or strategy against the `driver.Driver` port
  (`core/internal/driver/driver.go`), never against a concrete
  transport.
- **Never add a tool that duplicates an existing tool's intent.** The
  surface is deliberately small on purpose, because overlapping tools
  cause measurable agent confusion (see `CLAUDE.md`'s non-negotiable
  rules). Before adding a new tool under `core/internal/mcp`, confirm
  no existing tool (`tap`, `input_text`, `find_element`, and so on)
  can absorb the intent through a new parameter instead.
- **Do not inline business logic in a tool handler.** Every tool under
  `core/internal/mcp` decodes its arguments, then delegates to an
  `orchestra.Orchestra` method
  (`core/internal/orchestra/orchestra.go`). New behavior belongs on
  `Orchestra`, `Finder`, or `ScrollController`
  (`core/internal/orchestra/scroll_controller.go`), called from the
  tool handler, not pasted into the handler itself. Selector
  compilation lives in `core/internal/orchestra/priority_broadening.go`
  and the raw filter primitives in `core/internal/orchestra/filter.go`.
- **Never emit a tree or an element without the masking pass, and
  detect secrets only through `orchestra.IsSecure`.** Every driver sets
  `attributes["secure"] = "true"` on a node that holds a secret, and
  `IsSecure` is the only reader, never the role, because an Android
  password field (role `text-field`) and an iOS secure text field must
  take the same path, including the masked-count check in
  `WaitForInputCommitted`. A secret's `text` and `value` must reach no
  output as the real string. A tool result gets this from the tool-call
  middleware, the single masking mechanism for tool output, which masks
  every element object that reports `secure: true`, so a new result
  shape for tree or element data must carry the `secure` member
  (`core/internal/mcp/secure_result.go`). A value that a tool result
  builds from a node and that is not an element object, such as a
  selector, must leave out the secret field itself. Any output outside a
  tool result, such as an evidence file or the sidecar tree, must call
  `orchestra.MaskSecure`. Selector resolution keeps the real tree, so
  never mask before resolving.
- **Never log or return typed or captured values.** A `type` step may
  enter a secret, so its result detail states a character count only
  (`core/internal/runner/type_command.go`), and the step summary shows
  a mask of one fixed width for every input. A captured API response
  can hold tokens, so `extract` reports each variable name with the
  length of its value, and a failed `assertApi` or `assertExpr` describes
  an actual string by length instead of printing it
  (`describeCapturedValue` in `core/internal/runner/support.go`). Any
  new step result, log line or evidence file that would carry such a
  value must carry its length instead.

## Observation-driven input and waits

- **Never introduce hardcoded sleeps in Orchestra action pipelines.**
  Fixed sleeps are fragile across Flutter, React Native, and iOS.
  Driver adapters and the wait helpers in
  `core/internal/orchestra/wait.go` self-synchronize on focus and
  keyboard state, so Orchestra should not add its own waits without a
  concrete observable justifying them.
- **`InputText` taps the selector, then waits for focus, before typing
  or erasing.** `Orchestra.InputText`
  (`core/internal/orchestra/orchestra.go`) dispatches a focus-settling
  tap, then calls `WaitForFocus` so a following erase and type land in
  the field the caller aimed at rather than whichever field held focus
  before the tap. A focus that never reaches the target returns an
  `*InputFocusError` instead of typing into the wrong field. A new
  input-shaped action should follow the same tap-then-wait sequence.
- **The `maybeKeyboardOpen` session flag controls the tap
  keyboard-dismiss gate.** It is set after every `InputText`
  (`core/internal/orchestra/orchestra.go`). A new Orchestra action that
  implicitly opens the keyboard must set the flag too, or a subsequent
  tap will not dismiss it correctly on obscurement. The flag is
  cleared on `LaunchApp`, `StopApp`, and `HideKeyboard` to avoid stale
  state.
- **Selector ranking respects `Nth` as an opt-out.** Without `Nth`,
  `CompileSelector`
  (`core/internal/orchestra/priority_broadening.go`) ranks candidates
  by a clickable-plus-focused score. With `Nth` set, ranking is
  skipped and the Nth match in document order is returned instead,
  because `Nth` is positional by definition. A script that records a
  step where positional selection matters must keep the `Nth` in
  place.
- **Waits live in `core/internal/orchestra/wait.go`, not inline in
  Orchestra.** `WaitForFocus`, `WaitForInputReady`, and
  `WaitForInputCommitted` take a `Clock` for deterministic tests. Add
  a new wait here, following the same signature pattern, rather than
  inline inside an Orchestra method.
- **The script `handle` command polls, so do not add a `sleep` before
  it.** `HandleCommand`
  (`core/internal/runner/handle_command.go`) polls the current screen
  state at a 100ms interval up to its timeout (5s default) waiting for
  a branch's `when` condition to hold. A script that pads a fixed
  sleep before `handle` just wastes wall-clock time, so remove it.

## iOS clear-input

- **`ClearFocusedInputCommand` runs a 4-strategy chain, in order: a
  select-all-plus-delete keyboard shortcut, a triple-tap select, a
  long-press system edit menu, then an exact-length delete loop.** See
  `platforms/ios-agent/Tests/Commands/ClearFocusedInputCommand.swift`
  and the strategies under
  `platforms/ios-agent/Tests/Commands/Clear/Strategies/`. The chain
  polls the focused field after each attempt to confirm it is actually
  empty before advancing. The keyboard-shortcut fast path requires
  simulator hardware-keyboard pairing (`xcrun simctl keyboard <udid>
  enable hardware`, or Simulator, then I/O, then Keyboard). Without
  it, the shortcut types a literal character instead of selecting, and
  the chain falls through to the remaining strategies.

## iOS control plane

Order roughly follows severity, most likely to bite a new contributor
first.

### Process and state

- **iOS has no system-wide foreground query.** Unlike Android's
  `AccessibilityService.rootInActiveWindow`, XCUITest can only drive
  an app whose `XCUIApplication` reference the driver already holds.
  The Swift side tracks this in `DriverState.currentApp` /
  `currentBundleId`
  (`platforms/ios-agent/Tests/Commands/CommandHandler.swift`), and the
  Go side mirrors it in `IosDriver.lastLaunchedBundleID`
  (`core/internal/iosdriver/driver.go`). Never build a flow that
  assumes `CurrentForeground` reflects what a user manually opened. If
  the agent never called `LaunchApp`, the tracked state is empty and
  `CurrentForeground` returns a typed unsupported error rather than a
  guess.
- **`LaunchApp` resets app state.** `XCUIApplication.launch()` on an
  already-running app terminates and relaunches it, so any manually
  focused field, open sheet, or mid-flow state is lost. A caller that
  wants to attach to the running instance instead of resetting it
  passes `LaunchArgs.NoReset = true`.
- **Stale driver state after a simulator crash.** If the XCUITest
  process dies mid-session (simulator reboot, Xcode toolchain crash),
  the host adapter's tracked bundle id becomes stale. `Hierarchy`
  recovers on its next call by probing candidate bundle ids, but this
  is recovery, not prevention. A long-running session should poll
  `Ping` periodically to catch the failure earlier.

### Queries and timing

- **Never use per-element property access for bulk tree operations.**
  Each property read (`identifier`, `label`, `frame`) is a separate RPC
  to the XCUITest daemon, so walking a tree that way costs seconds on
  a few hundred elements. `DumpRawTreeCommand` and `XCUIBridge.getKeyboard`
  (`platforms/ios-agent/Tests/Bridge/XCUIBridge.swift`) instead call
  `app.snapshot()` once and walk the resulting `XCUIElementSnapshot`
  tree locally, one RPC with local, constant-time property access
  afterward.
- **`XCUIElementSnapshot` does NOT expose `isHittable`.** Hittability
  is a live screen-state query, not part of a snapshot. Code that
  needs a `clickable` signal must use a different heuristic. The
  adopted one is the elementType and accessibility-trait whitelist in
  `NormalizeIosTree` (`core/internal/iosdriver/normalize.go`).
- **`XCUIElementQuery.waitForExistence(timeout:)` can false-negative
  even on a short timeout**, returning false against a Flutter host
  where the element in question is actually present in the same
  tick's `app.snapshot()`. Prefer a snapshot walk over element-query
  polling for anything requiring reliable detection.
- **Unbounded recursion on deeply nested trees.** Flutter and React
  Native apps routinely nest 30 or more wrapper layers. A tree walker
  must be iterative (an explicit stack), not recursive, to avoid a
  Swift call-stack overflow on a pathological target. See
  `XCUIBridge.buildRawTree`'s two-pass, stack-based walk for the
  pattern.

### Gestures and tapping

- **iOS "back" is NOT a system primitive.** Android dispatches a BACK
  intent regardless of app handling, but iOS only has per-screen
  affordances. `BackKeyStrategy`
  (`platforms/ios-agent/Tests/PressKey/BackKeyStrategy.swift`) tries
  the nav bar back button first (verifiable, reports
  `affordanceFound: true`) then an edge-swipe fallback (unverifiable,
  reports `affordanceFound: false`). A caller must check that field
  and fall back to a screen-specific Cancel, Done, or Close affordance
  when it is false, never treat `PressKey(back)` as guaranteed the way
  Android allows.
- **Edge-swipe back needs precise gesture parameters.** A normalized
  offset `dx = 0` (not a small positive value) and a zero-duration
  press with a fast drag and no final hold are what the pop gesture
  recognizer accepts. A slower press or a nonzero final hold reads as
  a context-menu trigger instead of a navigation gesture. See
  `BackKeyStrategy`'s edge-swipe branch.
- **Coordinate-based tap is more reliable than `XCUIElement.tap()`
  for cross-platform targets.** The native `tap()` path performs
  extra preflight checks (wait for hittable, wait for stable) that
  fail silently against a Flutter or React Native element whose
  semantic node lacks proper hit-test metadata. `TapAtCommand`
  dispatches through `EventSynthesizer` at a coordinate instead, the
  same path selector-based taps compose through host-side.

### `XCUIElement.ElementType` naming

- **An unmapped `elementType` renders as `typeNN`, unreadable for
  agent role filtering.** `elementTypeName`
  (`platforms/ios-agent/Tests/Bridge/ElementTypeName.swift`) covers
  the documented `XCUIElement.ElementType` cases explicitly and falls
  through to `type\(rawValue)` via `@unknown default` for forward
  compatibility. When bumping the supported Xcode range, grep a smoke
  run's tree dump for `"type\d+"` to catch any element type Apple
  added that the mapping doesn't yet cover.

### Flutter and cross-platform assumptions

- **Flutter apps do NOT universally use a custom keypad.** Some apps
  use the iOS system keyboard, and native `app.typeText()` works
  directly. A custom-keypad fallback must check `getKeyboard` first
  and branch on visibility rather than assuming every Flutter app
  needs the fallback path.
- **Interactive-type empty wrappers are noise.** Some `button` or
  `cell` elements in a Flutter tree carry an empty identifier, label,
  and value. The Swift-side stable-signal filter keeps them because
  the elementType is in the interactive whitelist, but they carry
  nothing an agent can act on. A consumer that filters for actionable
  elements should drop these rather than surface them.

### Architecture constraints

- **Never try to run an HTTP server inside an iOS app.** The sandbox
  suspends the process within seconds of backgrounding. All transport
  is TCP from the XCUITest process (host-side).
- **Never change the iOS bridge strategy without explicit direction.**
  The Swift XCUITest runner is the committed approach. An alternative
  bridge is a large rewrite of the iOS transport layer and stays out
  of scope until the user reopens it.
- **Design any new capability against `driver.Driver`
  (`core/internal/driver/driver.go`), never with iOS-specific methods
  on the port.** The snapshot-based query strategy, the coordinate-only
  gesture surface, and the iterative tree walk all sit inside the
  adapter, never on the shared interface.
- **A simulator-backed driver and a device-backed driver cannot both
  serve port 22087 at once.** A simulator shares the host's network
  namespace and binds `127.0.0.1:22087` directly. A physical device
  needs `iproxy` to tunnel the same port over USB, and `iproxy` cannot
  bind the host side while the simulator already holds it.
  `launchIproxy` (`core/internal/iosdriver/iproxy.go`) probes the host
  port with a ping handshake before spawning `iproxy`, reusing an
  existing Atomyx tunnel it finds rather than refusing outright.
  Switching between simulator and device targets still requires
  stopping one driver before starting the other.
- **The XCUITest process and, for a physical device, the `iproxy`
  tunnel are two separate pieces of state, and exactly one owner starts
  each.** With a provisioner in `DriverFactoryDeps`,
  `DriverProvisioner.EnsureIOSAgent`
  (`core/internal/device/provisioner.go`) starts the runner on a port
  reserved for that target and, for a device, the tunnel as well, so the
  driver it feeds is built with `Options.ExternalTunnel` and starts no
  tunnel of its own. Without a provisioner (the sidecar), the runner is
  someone else's and `IosDriver.Connect`
  (`core/internal/iosdriver/iproxy.go`) starts the tunnel for a device
  target. Never start a tunnel from both sides.
- **Never share a derived data path between targets, and never build
  outside the build lock.** Every launch builds once into
  `platforms/ios-agent/build` under an exclusive lock on
  `build/.build.lock`, and takes the decision to build only after the
  lock is held. Each target then runs `test-without-building -xctestrun`
  from its own `build/run-<udid>` derived data path. A second build or a
  second runner writing into the same path corrupts the other's session.
- **A runner lives as long as its owning process, never as long as a
  session.** `IosDriver.Disconnect` and `Session.Close` leave the runner
  running. Every composition root that builds a `DriverProvisioner` without
  `FixedIosPort` must pass it a `device.RunnerKeeper` and run the keeper's cleanup
  (`runnerprovision.Cleanup`) on its normal exit path and in its stop
  watchdog, or the process leaks an `xcodebuild` process group. A runner
  stops at once only on a Connect failure, and through `Built.Release`
  for a caller that owns the driver outright, such as the Studio sidecar.
- **Never stop a tunnel or remove a record another process owns.** The
  provisioner's loss hook, the record removal and `RunnerKeeper.StopAll`
  all compare the record's `runnerId` and owner with their own, and leave
  the runner alone when the owner differs.
- **Long-lived child processes go through `BackgroundRunner`, never
  `Runner.Run`.** `iproxy` and the XCUITest runner
  (`xcodebuild test-without-building`) are started with
  `process.BackgroundRunner` (`core/internal/process/background.go`),
  because `Runner.Run` applies a 10 minute fallback timeout to any
  deadline-less context and would kill them mid-session. The driver and
  the launcher drain the child's stdout, stop it by killing its process
  group through the `BackgroundProcess` handle, and receive the runner
  by constructor injection from the composition roots.
- **`make serve-device` uses `test-without-building`, and its build
  stamp depends on the Swift sources and the project file.** After a
  change under `platforms/ios-agent/Tests/` or `App/`, the next
  `make serve` or `make serve-device` rebuilds before it serves. Calling
  `xcodebuild test-without-building` by hand skips that check and runs
  the stale binary from the previous build.
- **A Derived-Data or module-cache path breaks after the driver
  directory is renamed or moved.** Xcode's precompiled headers embed
  an absolute path to the module cache location. If
  `platforms/ios-agent` (or its build output) moves, the next build
  fails with a PCH-path mismatch error referencing `SwiftShims`. Fix:
  remove `platforms/ios-agent/build` entirely and re-run `make setup`,
  since the PCH embeds the old path in binary form and `xcodebuild`
  reuses it rather than detecting the mismatch on its own.
- **Private XCTest symbol drift falls back silently to the coordinate
  backend.** `EventRecordSynthesizer`
  (`platforms/ios-agent/Tests/Bridge/EventRecordSynthesizer.swift`)
  looks up `XCSynthesizedEventRecord`, `XCPointerEventPath`, and
  `XCTRunnerDaemonSession` via `NSClassFromString`. When Apple changes
  a selector's behavior between Xcode majors, the dry-run probe at
  load time catches it and `EventSynthesizerFactory` falls back to
  `CoordinateSynthesizer` instead. A multi-pointer script then starts
  failing right after an Xcode upgrade, which reads as "my script
  broke" until someone checks `ping`'s `probeLog` field, which names
  exactly which class or selector drifted. When adding new
  private-symbol access, keep it inside
  `platforms/ios-agent/Tests/Bridge/EventRecordSynthesizer.swift` (the
  one file allowed to touch those symbols), probe for it in the dry
  run, and document the expected signature next to the call so a
  version bump can diff against the current expectation. A grep for
  `NSClassFromString("XCSynth`, `NSClassFromString("XCPointer`, or
  `NSClassFromString("XCTRunner` across `platforms/ios-agent/Tests`
  must return matches only inside that one file. Any other match is an
  isolation violation.
- **The private synthesize completion block must be built in
  Objective-C, never in Swift.** A Swift `@convention(block) (NSError?)
  -> Void` (or any `AnyObject?`-typed variant) auto-retains its
  argument at block entry, and the XCTest daemon's XPC reply has been
  observed to invoke the block with a sentinel pointer instead of a
  real `NSError*` or nil, crashing on that auto-retain. The block must
  declare its parameter as `__unsafe_unretained id _Nullable` and must
  not close over any Swift value, because XPC's `_Block_copy` does not
  survive a Swift-captured closure. See
  `platforms/ios-agent/Tests/Bridge/AtomyxBlockHelper.h` and `.m`
  (wired through `SWIFT_OBJC_BRIDGING_HEADER` in
  `platforms/ios-agent/project.yml`) for the reference implementation,
  and dispatch the call from a background
  queue, because `synthesizeEvent:completion:` posts its completion
  block to main and a caller waiting on main would deadlock.
- **Gesture arbitration between nested scrollables is unreliable with
  a synthesized swipe.** A scrollable inside another scrollable can
  route an XCUITest-synthesized swipe intermittently to the outer
  scrollable even when the swipe coordinates land inside the inner
  one. A single MCP `swipe` call is reliable, while rapid consecutive
  `dispatchPointer` calls are not. Prefer a flat scrollable hierarchy
  in a test fixture where possible, or route through the `swipe` tool
  rather than a sequence of low-level dispatches.
- **Multi-pointer dispatch can fail mid-inertia.** The private
  synthesizer rejects a multi-pointer dispatch that arrives while a
  page-scroll inertia animation is still running: pinch, rotate, and
  two-finger gestures silently fail, with no error, but the app's own
  scale-gesture callback never fires. A caller should settle-wait at
  least a second between a page scroll and the next multi-pointer
  dispatch. A single-pointer dispatch does not need this wait.
- **Every synthesizer-using command must set `requiresMainThread =
  false`.** `EventRecordSynthesizer` guards against main-thread
  dispatch, because its completion block lands on main and waiting for
  it from main would deadlock.
  `CommandHandler.requiresMainThread` defaults to `true`, so a new
  gesture command that calls the synthesizer must explicitly opt out,
  matching `TapAtCommand`, `LongPressAtCommand`, `SwipeCommand`, and
  `DispatchPointerCommand`. Forgetting this returns a hard error from
  the synthesizer on the very first dispatch rather than hanging, but
  it is the kind of bug a contributor trips over silently when adding
  a new gesture command.

### Agent ergonomics for selector actions

- **Obscurement detection must treat an ancestor container as a
  non-obscurer.** A pre-order walk for "the topmost element containing
  this point" returns the deepest rendered element at that point,
  which on iOS is frequently the target's own ancestor (a collection
  view wrapping the cell wrapping the label). `DetectObscurement`
  (`core/internal/orchestra/obscurement.go`) checks whether the
  candidate obscurer's tree path is a prefix of the target's own path
  before flagging it, and separately suppresses a `container` or
  `other` role with no id and no label, because that combination is
  almost always an unstyled layout wrapper rather than a real
  obscurer.
- **Safe-area insets matter for "in viewport" checks.** A screen's
  reported bounds include the status bar, notch, and home indicator.
  An element whose midpoint sits in those edge zones is technically
  inside the rect but not reliably tappable. `ScrollController`
  (`core/internal/orchestra/scroll_controller.go`) uses conservative
  insets (60pt top, 50pt bottom by default) before deciding an element
  is visible, so the scroll loop keeps running until the element sits
  comfortably inside the interactive area.
- **A selector-based tap on a virtualized list needs a scroll-search
  phase, not only a positional scroll-into-view loop.** A list that
  recycles off-screen cells can report a logically present cell as
  absent from the current tree entirely. `ScrollController.scrollSearch`
  swipes the screen's center column in each direction for a small,
  fixed budget, probing the selector between each swipe, rather than
  forcing the caller to scroll manually. The budget stays small
  because this is a fallback, not the primary workflow, and unbounded
  scrolling is worse than a clear "could not find element" error.
- **Search bars ARE editable text inputs.** An editable-element
  whitelist that matches on class name must include `searchField`
  alongside `textField` and `secureTextField`, because iOS serializes
  a search field's class as `searchField`, which does not contain
  `textfield` as a substring. `interactiveIosRoles`
  (`core/internal/iosdriver/normalize.go`) includes it, and a new
  cross-platform "is this an input field" heuristic should too.
