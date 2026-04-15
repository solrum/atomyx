# Atomyx — Development workflow

Build, test, and extension recipes. For the repo layout see
[`repo-map.md`](./repo-map.md); for tool-specific rules see
[`tools.md`](./tools.md); for traps to avoid see
[`pitfalls.md`](./pitfalls.md); for the architectural contract see
[`architecture.md`](./architecture.md).

## Build / test (TypeScript monorepo)

Atomyx is an npm workspace. All TS packages live flat under
`packages/`. Root `package.json` has `"workspaces": ["packages/*"]`.

```bash
npm install               # installs root deps + symlinks @atomyx/* packages
```

Build packages in dependency order (core-driver → drivers → mcp → cli):

```bash
for d in core-driver core-driver-wire core-driver-android \
         core-driver-ios core-driver-mcp core-driver-cli; do
  (cd packages/$d && npx tsc)
done
```

Test a single package:

```bash
cd packages/core-driver
node --import tsx --test $(find src -name '*.test.ts')
```

Test all packages (from repo root):

```bash
for d in packages/core-driver*; do
  (cd "$d" && [ -f tsconfig.json ] && npm test)
done
```

Legacy `src/` runtime still exists alongside the new packages (being
phased out) and ships its own test suite:

```bash
npx tsc --noEmit          # typecheck legacy
npm test                  # 39 tests in src/
```

## iOS driver (Swift)

Primary interface is the Makefile in `native/ios-driver/`:

```bash
cd native/ios-driver
make setup                # xcodegen + build-for-testing (simulator)
make test                 # unit tests only (fast)
make serve                # start TCP server, blocks terminal
```

Physical device:

```bash
cp device.env.example device.env
$EDITOR device.env        # DEVICE_UDID + DEV_TEAM
make setup-device
make serve-device         # spawns iproxy tunnel
```

Swift source lives in `native/ios-driver/Tests/{Server,Bridge,Commands}/`.
15 commands cover the full `Driver` primitive set — see
[`native/ios-driver/README.md`](../../native/ios-driver/README.md) for
the command list.

**Pitfall**: after `git mv`-ing the native driver directory, nuke
`native/ios-driver/build/` before rebuilding. Xcode PCH embeds
absolute paths and throws `missing required module 'SwiftShims'`
otherwise.

## Android agent (Kotlin)

```bash
cd native/android-agent
./gradlew :app:assembleDebug
adb install -r app/build/outputs/apk/debug/app-debug.apk
```

Min SDK 26, target 34.

After reinstall, Android caches the old accessibility binding. To
re-bind:

```bash
PKG=dev.atomyx.agent
SVC=$PKG/$PKG.service.AtomyxAccessibilityService
adb shell 'settings delete secure enabled_accessibility_services'
adb shell settings put secure enabled_accessibility_services "$SVC"
adb shell am start-foreground-service -n $PKG/.control.AtomyxForegroundService
```

`@atomyx/core-driver-android` preflights for stale-binding state and
returns actionable rebind hints when `tree` comes back empty.

## End-to-end test (real device + MCP)

```bash
# 1. Start the driver (iOS simulator example)
cd native/ios-driver && make serve   # blocks

# 2. In another terminal, start the MCP server
cd packages/core-driver-cli
node dist/main.js mcp --platform ios --kind simulator

# 3. Wire into an MCP client (.mcp.json) and drive the device
```

Legacy smoke scripts still work for the old `src/` path:

```bash
bash scripts/smoke-device.sh      # curl device endpoints directly
node scripts/smoke-mcp.mjs        # legacy MCP stdio round trip
```

A new `scripts/atomyx-cli-smoke.sh` targeting the new CLI is planned.

## Extension checklists

### Adding a new MCP tool

All new tool work goes into `packages/core-driver-mcp/src/tools/`.
See [`tools.md`](./tools.md) for the full template. Short version:

1. Confirm no existing tool can absorb the intent (surface is
   deliberately small).
2. Create `packages/core-driver-mcp/src/tools/<name>.tool.ts` using
   `defineTool({ name, description, inputSchema, execute })`.
3. Import in `packages/core-driver-mcp/src/tools/index.ts` and append
   to `DEFAULT_TOOLS`.
4. Tool handler calls one or more `Orchestra` methods (nothing else).
5. Add a test in `packages/core-driver-mcp/src/server.test.ts`
   that dispatches through the MCP request handler and asserts
   the expected `MockDriver` calls.

### Adding a new Driver method (new primitive)

1. Add to `packages/core-driver/src/driver/driver.port.ts` with
   correct capability-flag semantics.
2. Implement in `packages/core-driver-ios/src/ios.driver.ts` and
   `packages/core-driver-android/src/android.driver.ts`.
3. For iOS, likely needs a new Swift command in
   `native/ios-driver/Tests/Commands/` too. Follow the command
   registry pattern in `AtomyxDriverUITests.swift`.
4. For Android, add an HTTP route in
   `native/android-agent/app/src/main/java/dev/atomyx/agent/control/`
   + the client call in `packages/core-driver-android`.
5. Add a test in `packages/core-driver/src/testing/mock-driver.ts`
   so all consumers can exercise the new method.
6. Compose higher-level behavior in
   `packages/core-driver/src/orchestra/orchestra.ts` or
   `packages/core-driver/src/finder/finder.ts` if needed.

### Adding a new filter primitive

1. Add to `packages/core-driver/src/filters/element-filter.ts` as a
   named export.
2. Unit test in `element-filter.test.ts` with fixture trees from
   `packages/core-driver/src/testing/fixtures.ts`.
3. Re-export from `packages/core-driver/src/filters/index.ts`.
4. If the filter needs a canonical attribute key that doesn't exist
   yet, add it to `AttrKeys` in `packages/core-driver/src/tree/tree-node.ts`
   AND to the driver normalizers
   (`packages/core-driver-ios/src/tree-normalizer.ts`,
   `packages/core-driver-android/src/tree-normalizer.ts`) so both
   platforms populate it.

### Adding a new selector strategy

Selector resolution is pure function composition now — no more
strategy classes. To add a new match mode:

1. Add a filter function to
   `packages/core-driver/src/filters/element-filter.ts` (e.g.
   `xpathMatches`, `boundsContainPoint`).
2. Compose it into `compileSelector` in
   `packages/core-driver/src/selectors/priority-broadening.ts` if
   it's a default strategy — or leave it as an exported primitive
   advanced users compose manually.
3. No device-side changes needed. Selector resolution runs entirely
   host-side now; the Swift/Kotlin drivers only produce raw trees.

## Testing philosophy

- **Mock the Driver, not the Orchestra.** `MockDriver` in
  `packages/core-driver/src/testing/` implements `Driver` with
  scripted hierarchy queues + call log. Feed it a sequence of
  hierarchies, drive your code, assert the call log.
- **Test handlers in isolation.** MCP tool tests dispatch through
  the actual `createMcpServer` handlers using `MockDriver` +
  `FakeClock` + `NoopLogger` — not reflecting on tool internals.
- **Unit tests are deterministic.** `FakeClock` means polling
  loops in `Finder` + `ScrollController` complete in microseconds
  under test; no `setTimeout` waiting.
- **Prefer `node:test` over jest.** Simpler, faster, no ESM
  headaches, no ts-jest. Run via `node --import tsx --test`.
- **Keep smoke scripts out of `npm test`.** `scripts/smoke-*` need
  a real device and aren't fast enough for unit runs.

## Workspace conventions

- Every package has its own `tsconfig.json` inheriting from a
  common target (no shared tsconfig base yet — add one if drift
  appears).
- Every package has its own test script: `node --import tsx --test $(find src -name '*.test.ts')`.
- Packages follow module-prefix naming: `@atomyx/core-driver-*`,
  `@atomyx/test-mgmt-*`, `@atomyx/studio-*`, `@atomyx/cloud-*`.
- Cross-package imports go through the public entry point only
  (`@atomyx/core-driver`, not `@atomyx/core-driver/src/...`).
  `dependency-cruiser` enforces this at CI time.
- Directory name = package name suffix without the `@atomyx/`
  scope. `packages/core-driver-mcp/` → `@atomyx/core-driver-mcp`.

## Release / publish

Not yet wired. When ready:

- Each `packages/*/package.json` publishes independently (`npm
  publish --workspace @atomyx/core-driver`).
- Changeset-based versioning is the planned tool.
- CI publishes on tag push.
- `@atomyx/core-driver-cli` is the primary end-user install target
  — it transitively pulls the drivers + MCP server.

See [`architecture.md`](./architecture.md) §6 for the planned
distribution model per persona.
