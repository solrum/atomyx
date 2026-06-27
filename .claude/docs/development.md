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

Build packages in dependency order (core → driver → drivers → mcp → script → cli):

```bash
for d in core driver driver-wire android-driver \
         ios-driver script mcp cli; do
  (cd packages/$d && npx tsc)
done
```

Test a single package:

```bash
cd packages/driver
node --import tsx --test $(find src -name '*.test.ts')
```

Test all packages (from repo root):

```bash
for d in packages/*/; do
  (cd "$d" && [ -f tsconfig.json ] && npm test)
done
```

## iOS driver (Swift)

Primary interface is the Makefile in `platforms/ios-agent/`:

```bash
cd platforms/ios-agent
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

Swift source lives in `platforms/ios-agent/Tests/{Server,Bridge,Commands,PressKey}/`.
The command set covers the full `Driver` primitive surface
(including `hideKeyboard` and `typeKey` for the ⌘A clear fast
path) — see [`platforms/ios-agent/README.md`](../../platforms/ios-agent/README.md)
for the authoritative command list.

**Pitfall**: after `git mv`-ing the platform driver directory, nuke
`platforms/ios-agent/build/` before rebuilding. Xcode PCH embeds
absolute paths and throws `missing required module 'SwiftShims'`
otherwise.

## Android agent (Kotlin)

```bash
cd platforms/android-agent
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

`@atomyx/android-driver` preflights for stale-binding state and
returns actionable rebind hints when `tree` comes back empty.

## End-to-end test (real device + MCP)

```bash
# 1. Start the driver (iOS simulator example)
cd platforms/ios-agent && make serve   # blocks

# 2. In another terminal, start the MCP server
cd packages/mcp
node dist/bin.js --platform ios --kind simulator

# 3. Wire into an MCP client (.mcp.json) and drive the device
```

For ad-hoc device smokes and MCP round-trip probes, check for a
`scripts/` directory on your checkout — it is gitignored and
contributors keep local-only experimental runners there. None
are part of the shipped surface.

## Extension checklists

### Adding a new MCP tool

All new tool work goes into `packages/mcp/src/tools/`.
See [`tools.md`](./tools.md) for the full template. Short version:

1. Confirm no existing tool can absorb the intent (surface is
   deliberately small).
2. Create `packages/mcp/src/tools/<name>.tool.ts` using
   `defineTool({ name, description, inputSchema, execute })`.
3. Import in `packages/mcp/src/tools/index.ts` and append
   to `DEFAULT_TOOLS`.
4. Tool handler calls one or more `Orchestra` methods (nothing else).
5. Add a test in `packages/mcp/src/server.test.ts`
   that dispatches through the MCP request handler and asserts
   the expected `MockDriver` calls.

### Adding a new Driver method (new primitive)

1. Add to `packages/driver/src/driver/driver.port.ts` with
   correct capability-flag semantics.
2. Implement in `packages/ios-driver/src/ios.driver.ts` and
   `packages/android-driver/src/android.driver.ts`.
3. For iOS, likely needs a new Swift command in
   `platforms/ios-agent/Tests/Commands/` too. Follow the command
   registry pattern in `AtomyxDriverAgent.swift`.
4. For Android, add an HTTP route in
   `platforms/android-agent/app/src/main/java/dev/atomyx/agent/control/`
   + the client call in `packages/android-driver`.
5. Add a test in `packages/driver/src/testing/mock-driver.ts`
   so all consumers can exercise the new method.
6. Compose higher-level behavior in
   `packages/driver/src/orchestra/orchestra.ts` or
   `packages/driver/src/finder/finder.ts` if needed.

### Adding a new filter primitive

1. Add to `packages/driver/src/filters/element-filter.ts` as a
   named export.
2. Unit test in `element-filter.test.ts` with fixture trees from
   `packages/driver/src/testing/fixtures.ts`.
3. Re-export from `packages/driver/src/filters/index.ts`.
4. If the filter needs a canonical attribute key that doesn't exist
   yet, add it to `AttrKeys` in `packages/driver/src/tree/tree-node.ts`
   AND to the driver normalizers
   (`packages/ios-driver/src/tree-normalizer.ts`,
   `packages/android-driver/src/tree-normalizer.ts`) so both
   platforms populate it.

### Adding a new selector strategy

Selector resolution is pure function composition now — no more
strategy classes. To add a new match mode:

1. Add a filter function to
   `packages/driver/src/filters/element-filter.ts` (e.g.
   `xpathMatches`, `boundsContainPoint`).
2. Compose it into `compileSelector` in
   `packages/driver/src/selectors/priority-broadening.ts` if
   it's a default strategy — or leave it as an exported primitive
   advanced users compose manually.
3. No device-side changes needed. Selector resolution runs entirely
   host-side now; the Swift/Kotlin drivers only produce raw trees.

## Testing philosophy

- **Mock the Driver, not the Orchestra.** `MockDriver` in
  `packages/driver/src/testing/` implements `Driver` with
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
- Packages follow a flat naming convention: `@atomyx/core`,
  `@atomyx/driver`, `@atomyx/mcp`, `@atomyx/cli`, etc.
- Cross-package imports go through the public entry point only
  (`@atomyx/driver`, not `@atomyx/driver/src/...`).
  `dependency-cruiser` enforces this at CI time.
- Directory name = package name suffix without the `@atomyx/`
  scope. `packages/mcp/` → `@atomyx/mcp`.

## Release / publish

Not yet wired. When ready:

- Each `packages/*/package.json` publishes independently (`npm
  publish --workspace @atomyx/driver`).
- Changeset-based versioning is the planned tool.
- CI publishes on tag push.
- `@atomyx/cli` is the primary end-user install target
  — it transitively pulls the drivers + MCP server.

See [`architecture.md`](./architecture.md) §6 for the planned
distribution model per persona.
