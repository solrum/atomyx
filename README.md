# adet — AI-Driven Exploratory Testing

> Drive real mobile devices from Claude (or any MCP client) to explore apps, record flows, and generate regression tests.
>
> **Cross-platform by design**: Android ships today (embedded control plane). iOS support is a **TODO** — the bridge approach has not been chosen yet. See [docs/ios.md](./docs/ios.md) for the open design questions. Self-contained, open source (Apache 2.0).

---

## What it does

`adet` exposes a small set of MCP tools that let a language model (or a YAML test spec) drive a mobile device **directly** using stable selectors resolved live on the device:

```
tap({ contentDesc: "Login" })
input_text({ resourceId: "com.example:id/email" }, "user@test.com")
type_via_keyboard("123456")
verify_state({ mustContain: ["Welcome", "Logout"] })
```

No test tree caching on the host. No ephemeral element IDs. One mental model for both platforms.

Two modes:

- **Mode B — scripted**: YAML spec → deterministic runner. Great for CI and regression.
- **Mode C — exploratory**: Claude drives the tools in a loop, reports bugs. Great for finding issues.

---

## Platform support

| Platform | Status       | Control plane                                     | Min version |
| -------- | ------------ | ------------------------------------------------- | ----------- |
| Android  | ✅ stable    | Embedded APK — NanoHTTPD + AccessibilityService   | API 26+ (screenshots require API 30+) |
| iOS      | 🟡 TODO      | **Approach not yet decided** — see [docs/ios.md](./docs/ios.md) | TBD         |

**Android** uses a custom lightweight control plane that runs directly on the device. No Appium. A single `adet` APK exposes an HTTP server on `127.0.0.1:8765`, accessed from the host via `adb forward`. Tree dumps are served from a cache invalidated by accessibility events — typical tap latency is ~30ms.

**iOS** is an open design question. iOS sandboxing prevents the Android-style embedded HTTP server approach, so the control plane will have to live on the host Mac — but *how* it talks to the device is undecided. Candidate approaches (Appium + WebDriverAgent, direct WDA, custom XCTest runner, `idb`, Accessibility Inspector, etc.) each have unresolved tradeoffs. The TypeScript tool layer, runner, explorer, and storage are already platform-agnostic via the `DeviceController` interface, so whichever approach is chosen slots in as a single adapter. See [docs/ios.md](./docs/ios.md) for the full comparison and the questions that need answering before implementation.

The MCP tool surface is designed to be **identical** between platforms once iOS lands — clients write one test spec, adet picks the right backend based on `select_device`.

---

## Layout

```
adet/
├── src/                           # MCP server (TypeScript) — platform-agnostic
│   ├── index.ts                   # stdio entry point
│   ├── server.ts                  # MCP request handler
│   ├── registry.ts                # Tool registration via factory
│   ├── types.ts                   # ToolDefinition generic
│   ├── runtime/
│   │   └── adet-context.ts        # DI container (history, results, controller)
│   ├── adapters/                  # Per-platform DeviceController implementations
│   │   ├── device-controller.port.ts  # Inspector + Actor + AppManager + Lifecycle interfaces
│   │   ├── agent-direct.adapter.ts    # Android: HTTP client → adet APK
│   │   ├── engine-appium.adapter.ts   # iOS: stub (approach TBD — filename is historical)
│   │   ├── device-router.ts           # select_device picks the right adapter
│   │   └── tree-diff.ts               # flattenText + treesEqual
│   ├── runner/                    # Mode B — YAML spec runner
│   │   ├── spec-schema.ts         # Zod schema
│   │   ├── var-resolver.ts        # ${data.x} / ${env.X:-default}
│   │   ├── spec-runner.ts         # Orchestrator (takes AdetContext)
│   │   └── steps/                 # Strategy registry: one handler per step type
│   │       ├── launch.handler.ts
│   │       ├── tap.handler.ts
│   │       ├── input.handler.ts
│   │       ├── swipe.handler.ts
│   │       ├── press-key.handler.ts
│   │       ├── wait-for-idle.handler.ts
│   │       ├── wait-for.handler.ts
│   │       ├── assert.handler.ts
│   │       └── sleep.handler.ts
│   ├── explorer/                  # Mode C — Claude API agent loop
│   │   ├── agent-loop.ts          # Anthropic SDK loop
│   │   └── system-prompt.ts       # Cached system prompt
│   ├── storage/                   # TestCase persistence (Strategy pattern)
│   │   └── test-case-storage.ts   # LocalFileStorage | EngineHttpStorage | CompositeStorage
│   ├── tools/                     # MCP tool categories
│   │   ├── tool-factory.ts
│   │   ├── devices.tools.ts
│   │   ├── ui.tools.ts
│   │   ├── actions.tools.ts
│   │   ├── app.tools.ts
│   │   ├── assertion.tools.ts
│   │   ├── verification.tools.ts
│   │   ├── runner.tools.ts
│   │   └── test.tools.ts
│   ├── cli/                       # Standalone CLI
│   │   ├── main.ts                # `adet run` / `adet list-devices` / `adet explore`
│   │   └── junit-reporter.ts      # CI-friendly output
│   └── testing/
│       └── mock-controller.ts     # In-memory mock for unit tests
│
├── android/                       # Android control plane (Kotlin APK)
│   └── ...                        # Self-contained Gradle project — see android/README.md
│
├── ios/                           # iOS control plane — TODO, empty today
│   └── README.md                  # Why it's empty + pointer to design doc
│
├── docs/
│   └── ios.md                     # iOS open design questions (approach TBD)
│
├── scripts/
│   ├── smoke-device.sh            # Device-layer smoke via curl (Android today)
│   └── smoke-mcp.mjs              # MCP stdio smoke test
│
├── CLAUDE.md                      # Instructions for Claude Code contributors
├── LICENSE                        # Apache 2.0
├── package.json
└── tsconfig.json
```

---

## Quick start (Android)

### 1. Install

```bash
# Control plane (device)
cd android
./gradlew :app:assembleDebug
adb install -r app/build/outputs/apk/debug/app-debug.apk
cd ..

# MCP server (host)
npm install
npm run build
```

### 2. Enable on device

Open the **adet** app. Tap through the permission flow:

1. **Accessibility** → toggle `adet` ON
2. **Usage Access** → grant `adet`
3. Return to app → tap **Enable adet control server**
4. Persistent notification appears: "adet — control server active"

> **Developer note**: after an APK update, Android caches the old binding. If the tree dump returns empty, run:
> ```bash
> adb shell 'settings delete secure enabled_accessibility_services'
> adb shell settings put secure enabled_accessibility_services "dev.solrum.adet.agent/dev.solrum.adet.agent.service.AdetAccessibilityService"
> ```
> End users installing once won't hit this.

### 3. Connect

```bash
adb forward tcp:8765 tcp:8765
curl http://127.0.0.1:8765/health
# {"ok":true,"accessibilityConnected":true}
```

### 4. Smoke test

```bash
bash scripts/smoke-device.sh      # curl /health, /tree, /find, /screenshot directly
node scripts/smoke-mcp.mjs        # full MCP stdio → device round trip
```

## Quick start (iOS)

🟡 **TODO — approach not yet decided.** The `DeviceController` interface is ready; the iOS adapter is a stub that throws. Before contributing code, read [docs/ios.md](./docs/ios.md) for the candidate approaches and open questions. A prototype + discussion is required before committing to an implementation path.

---

## Tool reference

| Category    | Tools                                                                                           |
| ----------- | ----------------------------------------------------------------------------------------------- |
| Devices     | `list_devices`, `select_device`                                                                 |
| UI          | `get_ui_tree` (full/compact), `resolve_selector`, `get_keyboard`, `get_screenshot`              |
| Actions     | `tap`, `tap_coordinates`, `swipe`, `input_text`, `type_via_keyboard`, `press_key`               |
| App         | `list_apps`, `launch_app`, `current_activity`                                                   |
| Assertions  | `wait_for_element`, `wait_for_idle`, `verify_state`                                             |
| Diagnostics | `snapshot_tree`, `get_tree_diff`, `get_history`                                                 |
| Run         | `start_run`, `finish_run`, `report_bug`, `report_finding`                                       |
| Recording   | `start_test_session`, `get_recorded_actions`, `save_as_test_case`                               |
| Automation  | `run_test_spec` (Mode B), `start_exploration` (Mode C, needs `ANTHROPIC_API_KEY`)               |

### Selector shape

```json
{
  "selector": {
    "resourceId": "com.example:id/btn_login",
    "contentDesc": "Login",
    "text": "Sign in",
    "textContains": "ign",
    "hint": "login",
    "nth": 0
  }
}
```

Strategies are tried in priority order on the device side (Android): `resourceId` → `text` → `contentDesc` → `textContains` → `hint`.

> **iOS selector mapping**: how these fields map to iOS attributes (`name` / `label` / `value` / `identifier` / etc.) depends on the chosen iOS bridge approach. adet intends to normalize them losslessly at the adapter layer so specs run unchanged across platforms — the details are a TODO tracked in [docs/ios.md](./docs/ios.md).

---

## Mode B — Scripted tests

Write YAML specs. The runner is deterministic and CI-friendly.

```yaml
# tests/login.yaml
name: Login happy path
target: com.example.app
data:
  email: ${env.TEST_EMAIL:-test@example.com}
  password: ${env.TEST_PASSWORD:-Test123!}

setup:
  - launch: ${target}
  - wait_for_idle: { timeoutMs: 3000 }

steps:
  - input:
      find: { hint: "Email" }
      text: ${data.email}
  - input:
      find: { hint: "Password" }
      text: ${data.password}
  - tap: { text: "Login" }
  - wait_for_idle: { timeoutMs: 5000 }

verify:
  mustContain: ["Welcome", "Logout"]
  mustNotContain: ["error", "invalid"]

bug_rules:
  - if: step_failed
    severity: critical
  - if: verify_failed
    severity: high
```

Run:
```bash
adet run tests/login.yaml --device=auto --report=junit.xml
```

---

## Mode C — Exploratory (Claude API)

Claude drives `adet` in a loop and reports bugs.

```bash
ANTHROPIC_API_KEY=sk-ant-... \
  adet explore \
    --app=com.example.app \
    --goal="find input validation bugs in the login screen" \
    --max-steps=30
```

Also available as the `start_exploration` MCP tool, so you can run it from inside an MCP client without the standalone CLI.

---

## TestCase storage (Strategy pattern)

`save_as_test_case` persists via one of 3 backends, auto-selected from env:

| Mode         | Trigger                              | Destination                              |
| ------------ | ------------------------------------ | ---------------------------------------- |
| `local`      | default (no env)                     | `~/.adet/test-cases/<id>.json`           |
| `engine`     | `ADET_STORAGE_MODE=engine`           | `POST ${ADET_ENGINE_URL}/...`            |
| `composite`  | `ADET_ENGINE_URL` set                | **Both** local + engine (best-effort)    |

Override local dir: `ADET_STORAGE_DIR=/path/to/dir`.

adet does not ship any engine — if you want cloud persistence, run your own HTTP server accepting the `TestCaseRecord` shape defined in `src/storage/test-case-storage.ts`.

---

## Architecture

```
 Claude Code / MCP client
        │ stdio
        ▼
┌────────────────────────────────────┐
│  adet MCP server (TypeScript)      │
│  ├── registry.ts (ToolFactory)     │
│  ├── runtime/AdetContext (DI)      │
│  ├── adapters/                     │
│  │   ├── AgentDirectCtrl (Android) │
│  │   └── iOS adapter (TODO)        │
│  ├── runner/ (Mode B)              │
│  ├── explorer/ (Mode C)            │
│  └── storage/ (Strategy)           │
└────────────────┬───────────────────┘
                 │
     ┌───────────┴───────────────┐
     │                           │
     ▼ adb forward                ▼ TODO — bridge approach TBD
┌─────────────────┐   ┌─────────────────────────┐
│ adet Android    │   │ (iOS control plane —    │
│ APK (Kotlin)    │   │  not yet implemented)   │
│ — NanoHTTPD     │   │  see docs/ios.md        │
│ — A11y service  │   │                         │
│ — SelectorChain │   │                         │
└────────┬────────┘   └─────────────────────────┘
         │
         ▼
    Android app
```

Key design decisions:

- **Selector-first everywhere**. No ephemeral element IDs cross the MCP boundary. Actions take a selector, resolved live on the device.
- **Platform-agnostic tool layer**. The MCP server, runner, explorer, and storage don't know about platforms — they talk to `DeviceController`. Adding iOS is implementing one adapter.
- **Cached UI tree on device (Android)**. Invalidated by `TYPE_WINDOW_*` accessibility events. Static screens serve from cache, typical tap latency ~30ms.
- **Strategy patterns throughout**. Step handlers, selector resolvers, storage backends, tool categories. Adding a new type = drop in a new file, no core edits.
- **Zero external coupling**. adet is fully standalone. No workspace inheritance, no shared tsconfig, no hidden dependencies.

---

## Tests

```bash
npm test
```

Uses `tsx` + `node:test` (no jest). Unit tests for var resolver, step handlers, storage strategy. Mock controller (`src/testing/mock-controller.ts`) lets you test handlers without a real device.

---

## Connect to Claude Code

Create `.mcp.json` at your project root:

```json
{
  "mcpServers": {
    "adet": {
      "command": "node",
      "args": ["/absolute/path/to/adet/dist/index.js"]
    }
  }
}
```

Restart Claude Code → `/mcp` lists `adet` with all 30 tools.

Then prompt: *"List connected devices, pick the first, launch com.example.app, open the login screen, and report a bug if the 'Sign in' button is not tappable."*

---

## Troubleshooting

| Symptom                                                    | Fix                                                                                                                   |
| ---------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------- |
| `/health` returns 503 `accessibilityConnected: false`      | Toggle adet in Accessibility settings OFF → ON. After APK updates, use the shell snippet in §2.                       |
| `/tree` returns empty list                                 | Accessibility service not bound to new APK. Toggle off + on.                                                          |
| `/screenshot` returns 500                                  | Device must be API 30+. Older devices don't have `AccessibilityService.takeScreenshot()`.                             |
| `/current-activity` returns empty                          | Grant Usage Access (`adb shell appops set dev.solrum.adet.agent android:get_usage_stats allow`).                      |
| Foreground notification disappears                         | Battery optimization killed the service. Add adet to "Don't optimize" in battery settings.                            |
| `tap` silently fails when keyboard is visible              | adet auto-dismisses keyboard if the target intersects the IME bounds. If your target is a keyboard key, pass it directly. |
| `type_via_keyboard` types wrong chars after a field focus  | The IME layout switches when you focus a different input type. adet re-fetches keyboard info before each type call.   |
| iOS device returns "not implemented"                       | iOS is 🟡 TODO — approach not yet decided. See [docs/ios.md](./docs/ios.md).                                           |

---

## Contributing

See [CLAUDE.md](./CLAUDE.md) for architecture principles and extension patterns.

**iOS support is an open design question** — see [docs/ios.md](./docs/ios.md). Before writing code, read the candidate approaches, prototype your preferred option for 2-3 days, and open a discussion issue with measurements + tradeoffs. Implementing the wrong approach is a multi-week rewrite; we want to decide before committing.

---

## License

Apache 2.0 — see [LICENSE](./LICENSE).
