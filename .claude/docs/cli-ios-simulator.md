# CLI iOS simulator launch plan

Deferred implementation. The `atomyx-driver mcp --platform ios
--kind simulator` path currently assumes the Swift XCUITest runner
is ALREADY listening on `127.0.0.1:22087`. Users hit this assumption
by running `make serve` in `native/ios-driver/` in a second terminal
before invoking the CLI — which is workable for development but a
bad first-run experience for anyone installing the framework fresh.

This doc captures the plan for making the CLI spawn the XCUITest
runner on demand so a single `atomyx-driver mcp ...` call Just
Works. Execution requires Xcode + `xcrun simctl` + a booted
simulator, which the repo's sandbox environment does not have —
hence "deferred". The plan is concrete enough that a contributor
with a local Mac can implement and smoke-test it in one batch.

## Current state

`packages/core-driver-cli/src/commands/mcp.ts` already:

1. Parses argv (`--platform ios --kind simulator --device <udid>`).
2. Constructs `IosDriver({kind, udid, port?})`.
3. Calls `driver.connect()` which opens a TCP socket to
   `127.0.0.1:22087` and sends a `ping` handshake.
4. Wires Orchestra + `createMcpServer` + StdioServerTransport and
   blocks until stdin closes.

What it does NOT do: verify the simulator is booted, verify the
XCUITest runner is running, or start it.

`native/ios-driver/Makefile` has `make serve` which does:

```
xcodebuild test \
  -scheme AtomyxDriver \
  -destination 'id=<udid>' \
  -only-testing:AtomyxDriverUITests/AtomyxDriverUITests/testServeCommands
```

Plus `AtomyxDriverUITests.testServeCommands` is a blocking test —
`RunLoop.current.run()` at the end keeps the XCUITest process alive
indefinitely, serving TCP commands on 22087 until the process is
killed.

The CLI-spawned path is conceptually: "do what `make serve` does,
but as a child process we can lifecycle from Node".

## Preconditions for the host Mac

The host running `atomyx-driver mcp` must have:

- Xcode + `xcrun` on PATH (`xcrun --version` succeeds).
- `xcodebuild` (comes with Xcode).
- A booted simulator whose UDID matches `--device`, OR a way for
  the CLI to boot one (see "simulator lifecycle" below).
- The Swift driver's `.xcodeproj` built against the current Xcode
  version. `xcodegen` must have been run at least once to emit the
  project; a fresh clone needs `make setup` first.

CLI must NOT try to `xcodegen generate` itself — that mutates files
in `native/ios-driver/` which is surprising for a consumer package.
If the `.xcodeproj` is missing, the CLI fails fast with a clear
error pointing the user at `cd native/ios-driver && make setup`.

## Proposed module layout

New file: `packages/core-driver-cli/src/ios-simulator-launcher.ts`

Responsibilities:

1. **Detect**: is the XCUITest runner already listening on 22087?
   If yes, skip spawn — reuse the existing process. Useful for
   developers who already ran `make serve` and want to hot-reload
   the CLI side.

2. **Enumerate simulators**: call `xcrun simctl list devices booted
   --json`, parse, return the list. If `--device` is unset, pick
   the single booted simulator; if multiple are booted, fail with
   a clear "pass --device UDID to disambiguate".

3. **Boot if needed**: if `--device` is set but that simulator is
   not booted, call `xcrun simctl boot <udid>`. Open
   `Simulator.app` to surface the UI window for visual debugging
   (use `open -a Simulator` — non-blocking).

4. **Locate xcodeproj**: walk up from the CLI's `__dirname` to
   find the repo root containing `native/ios-driver/AtomyxDriver.xcodeproj`.
   If not found (e.g. the CLI is `npm install`ed as a dependency
   outside the repo), fall back to an env var
   `ATOMYX_IOS_DRIVER_PROJECT` pointing at the built project, and
   if that's also unset, error out with:

   > iOS driver project not found. Build it once locally with
   > `git clone atomyx && cd atomyx/native/ios-driver && make setup`,
   > then set `ATOMYX_IOS_DRIVER_PROJECT=/absolute/path/to/AtomyxDriver.xcodeproj`
   > in your environment.

5. **Spawn xcodebuild**: `child_process.spawn("xcodebuild", [...])`
   with stdio piped. Track the child PID for shutdown. Tee stderr
   output to the ConsoleLogger at `debug` level so users running
   with `--log-level debug` can see the runner's boot sequence.

6. **Wait for port bind**: poll `127.0.0.1:22087` via
   `net.connect()` + short timeout, retry every 250ms for up to
   90s (xcodebuild cold starts on M1 ~30s, on Intel ~60s, plus
   test launch overhead). If the child exits before the port
   binds, log the captured stderr and throw.

7. **Hand off to `runMcp`**: the existing mcp command does the
   rest. The launcher just ensures 22087 is reachable before
   `IosDriver.connect()` runs.

8. **Shutdown**: the existing `SIGINT` / `SIGTERM` handler in
   `runMcp` calls `driver.disconnect()`. Extend it to ALSO kill
   the spawned xcodebuild child (if any). Use `SIGTERM` first,
   give it 5s to exit gracefully (the Swift driver's
   `RunLoop.current.run()` doesn't install its own signal handler
   so `SIGTERM` bubbles up and terminates xcodebuild), then
   `SIGKILL` as a safety net.

## Wiring into `runMcp`

`commands/mcp.ts` change sketch:

```ts
export async function runMcp(argv: ParsedArgv): Promise<void> {
  const logger = new ConsoleLogger(argv.logLevel ?? "info");

  // NEW: for iOS simulator kind, ensure the Swift runner is up
  // before we construct the driver. Launcher is responsible for
  // detect-or-spawn + port-bind wait + lifecycle tracking.
  let launcher: IosSimulatorLauncher | null = null;
  if (argv.platform === "ios" && (argv.kind ?? "simulator") === "simulator") {
    launcher = new IosSimulatorLauncher({
      udid: argv.device,
      port: argv.port ?? 22087,
      logger,
    });
    await launcher.ensureRunning();
  }

  const driver = createDriver(argv);
  ...
  const shutdown = async (signal: string) => {
    logger.info("shutting down", { signal });
    try { await driver.disconnect(); } catch (err) { ... }
    try { await launcher?.shutdown(); } catch (err) { ... }
    process.exit(0);
  };
  ...
}
```

The launcher is a no-op for Android and for iOS `kind=device` (the
physical device path uses iproxy which `IosDriver.connect` already
handles). Only `iOS simulator` gets the launch-then-connect path.

## Unit testing without a simulator

`IosSimulatorLauncher` should be constructed with injectable
command runners so unit tests can mock xcrun / xcodebuild:

```ts
export interface LauncherDeps {
  readonly logger: Logger;
  readonly exec: (cmd: string, args: string[]) => Promise<{stdout: string; stderr: string}>;
  readonly spawn: (cmd: string, args: string[]) => ChildProcess;
  readonly isPortOpen: (port: number) => Promise<boolean>;
  readonly sleep: (ms: number) => Promise<void>;
}
```

Test coverage (all hermetic — no real processes):

- Port already open → detect() returns "reuse", no spawn called.
- `simctl list devices booted --json` returns one device, no udid
  given → picks the single booted device.
- Zero booted, no udid → throws with "boot a simulator first".
- Multiple booted, no udid → throws "pass --device to disambiguate".
- `--device` UDID not in booted list → spawns `simctl boot <udid>`.
- xcodebuild child exits before port binds → captured stderr in
  the error message, SIGKILL sent to clean up.
- Successful launch → port poll succeeds within timeout, returns
  cleanly. Shutdown kills the child.

These all mock the exec/spawn/isPortOpen/sleep deps. Real
xcodebuild is never invoked. The test file goes in
`packages/core-driver-cli/src/ios-simulator-launcher.test.ts`.

## Smoke test on a real Mac

After the launcher lands, the manual verification a contributor
with Xcode runs:

```bash
# Fresh clone, never ran the iOS runner before:
git clone atomyx && cd atomyx
cd native/ios-driver && make setup && cd ../..

# Boot a simulator:
xcrun simctl boot "iPhone 16 Pro"
open -a Simulator

# Start the MCP server via the CLI — the CLI spawns the XCUITest
# runner itself, waits for it, then serves MCP stdio.
npm install
node packages/core-driver-cli/dist/main.js mcp \
  --platform ios --kind simulator \
  --device "$(xcrun simctl list devices booted --json | jq -r '.devices | to_entries[0].value[0].udid')" \
  --log-level debug
```

Expected log sequence:

```
[info] launcher: detecting simulator udid=... 
[debug] launcher: xcodebuild not listening yet, spawning...
[debug] launcher: xcodebuild pid=12345
[debug] launcher: polling 127.0.0.1:22087 ... not ready
[debug] launcher: polling 127.0.0.1:22087 ... not ready
[info] launcher: XCUITest runner ready (took 35000ms)
[info] connecting driver platform=ios kind=simulator
[info] driver connected
[info] MCP server starting on stdio
```

Press Ctrl+C — should see:

```
[info] shutting down signal=SIGINT
[info] driver disconnected
[info] launcher: killing xcodebuild pid=12345
[info] launcher: xcodebuild exited cleanly
```

Xcode's simulator window should still be open (we don't shut it
down — that's a user-initiated action). A subsequent
`atomyx-driver mcp ...` invocation should detect no runner on
22087 and re-spawn. No PID files, no lock files, no ambient state.

## Risks + alternatives considered

**Alternative A — tell the user to run `make serve` themselves.**
This is the current state. Works for developers, terrible for
anyone installing from npm and expecting `atomyx-driver` to be
self-contained. Rejected for the deferred batch because the whole
point of this work is removing that manual step.

**Alternative B — package a pre-built XCUITest bundle.** Ship the
compiled `.xctest` so the CLI doesn't need to invoke xcodebuild.
Rejected because code signing is contributor-specific — the bundle
baked into the npm package wouldn't match the user's Dev Team
identity and would fail to launch on their machine. `xcodebuild
test` with the user's signing config is the only portable path.

**Alternative C — spawn a persistent daemon with its own lifecycle
and have the CLI connect without spawning.** More complex state to
manage (PID files, stale lock detection, orphan cleanup) without a
clear benefit over the "spawn child, track it, kill on exit"
model. Rejected on YAGNI grounds.

**Risk: xcodebuild startup cost.** 30–90s is a real wait.
Mitigation: the "reuse running runner" path covers developers who
leave the process up between CLI invocations, and the launcher
surfaces intermediate progress at `debug` level so users can see
it's not hung. If this becomes a usability issue, a future
optimization is `--keep-runner` which leaves the xcodebuild child
running past CLI shutdown, tracked via a PID file in a well-known
location.

**Risk: xcodebuild process not killable via SIGTERM.** Some Xcode
versions ignore SIGTERM on the xctest process. Mitigation: the
shutdown path escalates to SIGKILL after a 5s grace window, and a
final fallback uses `xcrun simctl terminate <udid>
dev.atomyx.driver.host.tests` to force-kill the XCUITest app from
the simulator side.

## Execution checklist (when this batch lands)

1. Create `packages/core-driver-cli/src/ios-simulator-launcher.ts`
   with the `IosSimulatorLauncher` class + `LauncherDeps` interface
   per "Unit testing" above.
2. Create `packages/core-driver-cli/src/ios-simulator-launcher.test.ts`
   with the 7 test cases listed. Run under `npm test` — all green
   without touching real xcrun.
3. Modify `packages/core-driver-cli/src/commands/mcp.ts` to
   instantiate the launcher for iOS simulator kind and hook its
   shutdown into the SIGINT/SIGTERM handler.
4. Build core-driver-cli + run the workspace test suite.
5. Smoke test on a local Mac per "Smoke test on a real Mac".
6. Update `native/ios-driver/README.md` to document that the CLI
   can now spawn the runner, and keep `make serve` as the
   contributor dev loop for anyone working ON the runner.
7. Commit as a single batch titled
   `cli: spawn iOS XCUITest runner on demand for simulator mode`.

## Relation to Android

Android already works this way: `AndroidDriver.connect()` calls
`adbForward` which spawns `adb forward tcp:8765 tcp:8765` via a
child process, and the APK is assumed to be already installed on
the device (like the Swift driver assumes the xcodeproj is built).
No equivalent "boot the APK server" step exists because the APK's
Accessibility Service is always running once the user enables it
in Settings — there's no per-session process to spawn.

This batch brings iOS to parity in terms of "the CLI handles the
transport tunnel itself" at the cost of adding an xcodebuild
child. Android's adb forward is a 10ms operation; iOS's
xcodebuild+XCUITest cold start is 30+s. The asymmetry is
irreducible and worth documenting in the user-facing README.
