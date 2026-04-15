# adet — Development workflow

Build, test, and extension recipes. For the high-level map see
[`architecture.md`](./architecture.md); for tool-specific rules see
[`tools.md`](./tools.md); for traps to avoid see [`pitfalls.md`](./pitfalls.md).

## Build / test

### TypeScript

```bash
npm install
npm run build            # tsc, outputs to dist/
npm test                 # tsx + node:test, no jest
npm run dev              # tsc --watch
```

Test files live in `src/**/*.test.ts` and `src/testing/` — both are excluded
from the compile output via `tsconfig.json`.

### Android

```bash
cd android
./gradlew :app:assembleDebug
adb install -r app/build/outputs/apk/debug/app-debug.apk
```

After reinstall, Android caches the old accessibility binding. To re-bind:

```bash
PKG=dev.solrum.adet.agent
SVC=$PKG/$PKG.service.AdetAccessibilityService
adb shell 'settings delete secure enabled_accessibility_services'
adb shell settings put secure enabled_accessibility_services "$SVC"
adb shell am start-foreground-service -n $PKG/.control.AdetForegroundService
```

`launch_app` preflights for this stale-binding state and returns an actionable
rebind hint when `tree` comes back empty and `currentActivity` is unknown.

### End-to-end smoke

```bash
adb forward tcp:8765 tcp:8765
bash scripts/smoke-device.sh      # curl device endpoints directly
node scripts/smoke-mcp.mjs        # full MCP stdio → device round trip
```

## Extension checklist

### Adding a new MCP tool

1. **First, confirm no existing tool can be extended to cover the intent.**
   Tool surface is deliberately small (19 tools).
2. Create `src/tools/<name>.tool.ts`, export a class extending
   `Tool<{ args: MyArgs; result: MyResult }>`.
3. Constructor-inject strategies from `src/tools/core/` (add new strategies
   there if needed, don't inline).
4. `execute()` is orchestration only — every business rule must delegate to an
   injected strategy.
5. In `src/registry.ts`, instantiate and register via
   `factory.registerTool(new MyTool(...))`.
6. `execute()` receives `args` (validated by MCP against `schema`) and
   `ctx: AdetContext`. Access `ctx.controller` via `requireController(ctx)`.
7. Keep `description` short — offload details to `get_playbook`.
8. If the tool mutates device state, add its name to `MUTATING_TOOLS` in
   `src/registry.ts`. The server dispatcher auto-invalidates the UI cache and
   records the action after any mutating tool.
9. Add a test — for class-based tools test the class directly with
   `MockController`; for strategies, unit-test the strategy in isolation.

**Trivial exception**: tools that are pure delegation with no orchestration
(e.g. `press_key`) may live in `src/tools/trivial.tools.ts` batched together
rather than one file per tool. If they grow orchestration, promote to their
own `<name>.tool.ts`.

### Adding a new step type (Mode B runner)

1. Extend the Zod spec schema in `src/runner/spec-schema.ts`.
2. Create `src/runner/steps/<kind>.handler.ts` implementing `StepHandler`.
3. Export it from `src/runner/steps/index.ts` in `stepHandlers`.
4. Add a test in `src/runner/steps/<kind>.handler.test.ts`.

### Adding a new selector strategy (Android, device side)

1. Add the field to `SelectorResolver.Selector` data class in
   `android/.../control/SelectorResolver.kt`.
2. Implement `ResolutionStrategy` in a new file under
   `android/.../control/strategy/`.
3. Add it to `SelectorResolver.defaultStrategies()` in the desired priority
   position.
4. Also add the field to the host-side `Selector` type in
   `src/adapters/device-controller.port.ts` so specs can use it.
5. **Plan the iOS mapping** in `docs/ios.md`.

### Adding a new HTTP endpoint (Android control plane)

1. Implement `Route` in `android/.../control/router/` (add to `CommonRoutes.kt`
   or a new file).
2. Register the class in `HttpControlServer.buildRoutes()`.
3. Update `src/adapters/agent-direct.adapter.ts` with the client-side HTTP call.
4. If the new capability should be exposed to the tool layer, add a method to
   the relevant sub-interface in `src/adapters/device-controller.port.ts`, and
   implement it in **both** the Android adapter and the iOS stub.

### Adding a new storage backend

1. Implement `TestCaseStorage` in `src/storage/test-case-storage.ts`.
2. Update `resolveTestCaseStorage()` to handle your new env var / selector.
3. Add a test in `src/storage/test-case-storage.test.ts`.

## Storage modes

`save_as_test_case` logic (wired through `ctx.recordedActions`) uses a
strategy resolved from env vars:

| Env                                           | Effective storage                   |
| --------------------------------------------- | ----------------------------------- |
| (none)                                        | `LocalFileStorage` → `~/.adet/test-cases/` |
| `ADET_ENGINE_URL=http://...`                  | `CompositeStorage` (local + engine) |
| `ADET_STORAGE_MODE=engine ADET_ENGINE_URL=..` | `EngineHttpStorage` only            |
| `ADET_STORAGE_DIR=/custom/path`               | Overrides local dir                 |

The `engine` backend POSTs to a user-supplied HTTP endpoint. adet does not
ship any engine — if you want cloud persistence, run your own HTTP server
accepting the `TestCaseRecord` shape defined in
`src/storage/test-case-storage.ts`.

## Testing philosophy

- **Mock the device, not the tool layer**. `MockController` implements
  `DeviceController` with canned responses.
- **Test handlers in isolation**. Pass
  `{ controller: mockCtl, history, results, snapshots, recordedActions,
  invalidateUiCache: () => {}, lastToolName: null }` and assert `ctl.calls`.
- **Prefer `node:test` over jest**. Simpler, faster, no ESM headaches, no
  ts-jest.
- **Keep smoke scripts separate from unit tests**. `scripts/smoke-*.{sh,mjs}`
  need a real device; they're not part of `npm test`.

## Playbook + case studies

Tool-selection guidance has two persistence layers:

- **Playbook** — static decision tree in `src/tools/playbook-tools.ts`.
  Ship-versioned. Updated by hand when adding/removing tools or discovering
  new patterns.
- **Case studies** — `.adet/case-studies/YYYY-MM.md` (gitignored). Agents call
  `add_case_study` after recovering from a non-obvious error.
  `get_case_studies` reads them at the start of a new session.

When you change the tool surface, update the playbook markdown in
`src/tools/playbook-tools.ts` so future sessions see the new tool map
immediately.
