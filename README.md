# adet — AI-Driven Exploratory Testing

> Drive real mobile devices from Claude (or any MCP client) to explore apps, record flows, and generate regression tests.
>
> **Cross-platform by design**: Android ships today (embedded control plane). iOS support is a **TODO** — the bridge approach has not been chosen yet. See [docs/ios.md](./docs/ios.md). Self-contained, open source (Apache 2.0).

---

## What it does

`adet` exposes **19 MCP tools** that let a language model (or a YAML test spec) drive a mobile device using stable selectors resolved live on the device:

```ts
// Launch + discover selectors in one call
const { inputs, initialTree } = await launch_app({ packageName: "com.example.app" });

// Type into fields — selector OR coordinates, auto-clears by default
await input_text({ selector: { contentDesc: "Email" }, text: "user@test.com" });
await input_text({ x: inputs[1].center.x, y: inputs[1].center.y, text: "hunter2" });

// Tap with transition verification for navigation / login / submit
await tap_and_wait_transition({
  selector: { contentDesc: "Login" },
  waitForAbsent: { resourceId: "login_form" },
});
```

No test-tree caching on the host. No ephemeral element IDs. One mental model for both platforms. Selector priority is **resourceId > contentDesc > text > textContains > hint** — and the tool layer auto-broadens across types so you don't have to know whether Android exposes a given label as `contentDesc` or `text`.

Two modes:

- **Mode B — scripted**: YAML spec → deterministic runner. Great for CI and regression.
- **Mode C — exploratory**: Claude drives the tools in a loop, reports bugs. Great for finding issues.

---

## Platform support

| Platform | Status       | Control plane                                     | Min version |
| -------- | ------------ | ------------------------------------------------- | ----------- |
| Android  | ✅ stable    | Embedded APK — NanoHTTPD + AccessibilityService   | API 26+ (screenshots require API 30+) |
| iOS      | 🟡 TODO      | **Approach not yet decided** — see [docs/ios.md](./docs/ios.md) | TBD         |

**Android** uses a lightweight control plane running directly on the device. No Appium. A single `adet` APK exposes an HTTP server on `127.0.0.1:8765`, accessed from the host via `adb forward`. Tree dumps are served from a cache invalidated by accessibility events — typical tap latency is ~30ms. Handles native Android, Flutter, Compose, and React Native transparently (walk fallbacks for non-qualified `resourceId`, structural input detection, custom keypad handling).

**iOS** is an open design question. iOS sandboxing prevents the Android-style embedded HTTP server, so the control plane must live on the host Mac. Candidates (Appium + WebDriverAgent, direct WDA, custom XCTest target, `simctl`, `idb`) have unresolved tradeoffs. The TypeScript tool layer is already platform-agnostic via `DeviceController`; whichever approach is chosen slots in as one adapter. See [docs/ios.md](./docs/ios.md).

---

## Tool reference (19 tools)

> For full signatures, response shapes, shared patterns, and invariants, see **[`docs/tools.md`](./docs/tools.md)** — the reference agents should read before modifying anything under `src/tools/`.


| Category  | Tools                                                                 |
| --------- | --------------------------------------------------------------------- |
| Device    | `list_devices`, `select_device`                                       |
| App       | `launch_app` *(forceStop=true default, returns `inputs[]` + tree)*, `list_apps` |
| Screen    | `get_ui_tree`, `find_element`, `get_screenshot`                       |
| Actions   | `tap`, `tap_and_wait_transition`, `input_text`, `swipe`, `press_key`  |
| Wait      | `wait_for_element`                                                    |
| Run       | `start_run`, `finish_run`, `report_bug`                               |
| Guidance  | `get_playbook`, `add_case_study`, `get_case_studies`                  |

### One tool per intent

Each action has **exactly one** tool — agents don't have to choose between overlapping options:

- **Type text**: `input_text` (accepts `{selector, text}` OR `{x, y, text}`). Handles native EditText (ACTION_SET_TEXT), structural `find-input` strategy chain when selector points at a label/container, custom Flutter keypads (on-screen keys scan), system IMEs with layout switching.
- **Tap**: `tap` (accepts `{selector}` OR `{x, y}`). Use `tap_and_wait_transition` for any navigation / submit / login / network call — it verifies the transition, auto-extends on loading indicators, and classifies failures (dialog / loading / partial / no-change) with actionable hints.
- **Find**: `find_element` unifies lookup. Accepts exact (`resourceId` / `contentDesc` / `text`), substring (`labelContains`), cross-language `keyword`, role filter, `nth` / `nthOfRole` for positional disambiguation, `inputField: true` for the find-input strategy chain, `all: true` for lists. 2s result cache.

### Selector shape

```json
{
  "resourceId": "com.example:id/btn_login",
  "contentDesc": "Login",
  "text": "Sign in",
  "textContains": "ign",
  "hint": "login",
  "nth": 0
}
```

Priority: **resourceId > contentDesc > text > textContains > hint**. On Android, Material/Compose buttons typically set `contentDesc` and leave `text` empty — agents don't need to know: `tap` auto-broadens across types internally (tries `contentDesc` first even when you pass `text`, etc).

### Tree dump format

`get_ui_tree` returns a compact list sorted by selector stability with inline center coords. Each line uses explicit JSON-like form so agents can copy values verbatim:

```
resourceId="com.example:id/login_btn" button "Login" @540,1487
contentDesc="ログイン" button @540,1442
text="保存"  @100,200
contentDesc="注文"  @540,157 (2×)          ← duplicate — disambiguate by coords or nth
resourceId="G01-05-01/2"  @410,487
```

- `(N×)` = duplicate selector marker — agent must disambiguate via coords or `nth`
- `@cx,cy` = first-class selector — always valid, no resolver roundtrip
- `resourceId=""` preserves the full id (Flutter ids like `G01-05-01/2` and Android `com.pkg:id/foo` both work)

### `launch_app` returns `inputs[]` directly

```ts
const result = await launch_app({ packageName: "..." });
// result.inputs[] = [
//   { label: "口座番号", stableId: "acct_field", center: { x: 410, y: 990 }, currentValue: null },
//   { label: "パスワード", stableId: "password_field", center: { x: 410, y: 1139 }, currentValue: "••••••" },
// ]
// result.initialTree = "..."  // stable-only compact tree for buttons / other elements
```

Labels come from a structural walk (preceding-sibling / parent / descendant strategy chain), not positional index — register / login / settings flows with different field orders all work by matching labels semantically.

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

### 2. Enable accessibility

Open the **adet** app and tap through the permission flow (Accessibility → toggle `adet` ON, Usage Access → grant).

> **Developer note**: after an APK update, Android sometimes caches the old binding. If `get_ui_tree` returns empty:
> ```bash
> PKG=dev.solrum.adet.agent
> SVC=$PKG/$PKG.service.AdetAccessibilityService
> adb shell 'settings delete secure enabled_accessibility_services'
> adb shell "settings put secure enabled_accessibility_services $SVC"
> adb shell "am start-foreground-service -n $PKG/.control.AdetForegroundService"
> ```

### 3. Connect

```bash
adb forward tcp:8765 tcp:8765
curl http://127.0.0.1:8765/health
# {"ok":true,"accessibilityConnected":true}
```

### 4. Smoke test

```bash
bash scripts/smoke-device.sh      # curl /health, /tree, /resolve, /screenshot
node scripts/smoke-mcp.mjs        # full MCP stdio → device round trip
```

## Quick start (iOS)

🟡 **TODO — approach not yet decided.** The `DeviceController` interface is ready; the iOS adapter is a stub that throws. Read [docs/ios.md](./docs/ios.md) before contributing code. A prototype + discussion is required before committing to an implementation path.

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

steps:
  - input:
      find: { contentDesc: "Email" }
      text: ${data.email}
  - input:
      find: { contentDesc: "Password" }
      text: ${data.password}
  - tap: { contentDesc: "Login" }
  - wait_for: { contentDesc: "Welcome", timeoutMs: 5000 }
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

---

## TestCase storage (Strategy pattern)

Mutating actions are recorded into `ctx.recordedActions` and can be persisted via a storage strategy auto-selected from env:

| Mode         | Trigger                              | Destination                              |
| ------------ | ------------------------------------ | ---------------------------------------- |
| `local`      | default (no env)                     | `~/.adet/test-cases/<id>.json`           |
| `engine`     | `ADET_STORAGE_MODE=engine`           | `POST ${ADET_ENGINE_URL}/...`            |
| `composite`  | `ADET_ENGINE_URL` set                | **Both** local + engine (best-effort)    |

Override local dir via `ADET_STORAGE_DIR=/path/to/dir`. adet does not ship any engine — run your own HTTP server accepting the `TestCaseRecord` shape in `src/storage/test-case-storage.ts`.

---

## Playbook + Case studies

Tool-selection guidance lives in two places so it survives across sessions:

- **`get_playbook`** — static decision tree (typing, tapping, waiting, cross-language, error recovery). Ship-versioned, loaded from code. Call at session start when unsure.
- **`add_case_study({title, trigger, solution, example})`** — append a learned lesson to `.adet/case-studies/YYYY-MM.md`. Use after recovering from a non-obvious error. Reviewable by humans, picked up by future sessions via `get_case_studies`.

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
│  ├── storage/ (Strategy)           │
│  └── tools/                        │
│      ├── tree-render (tokens)      │
│      ├── find-input (strategy)     │
│      ├── preflight (a11y health)   │
│      └── playbook + case studies   │
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
│ — ResourceId    │   │                         │
│   strategy +    │   │                         │
│   walk fallback │   │                         │
│ — typeViaKeyb   │   │                         │
│   with Flutter  │   │                         │
│   keypad scan   │   │                         │
└────────┬────────┘   └─────────────────────────┘
         │
         ▼
    Target app
```

### Key design decisions

- **One tool per intent**. After consolidation from ~40 to 19 tools, agents don't face ambiguous choices. `input_text` is the one input tool; `tap` is the one tap tool; `find_element` is the one query tool.
- **Selector-first, coordinate-first-class**. Selectors cross the MCP boundary; ephemeral element IDs do not. But `@cx,cy` coords from `get_ui_tree` are ALSO first-class selectors — pass them directly to `tap` / `input_text` when no stable id exists.
- **Priority broadening across selector types**. The tool layer tries selector types in priority order (resourceId → contentDesc → text → …) regardless of which the agent passed. `tap({text:"OK"})` auto-matches an element with `contentDesc="OK"` — agent doesn't need to know platform conventions.
- **Flutter / Compose / RN first-class**. Non-qualified resourceIds (`G01-05-01/2`) resolve via a tree-walk fallback in `ResourceIdStrategy`. `clickable` flag is ignored when deciding whether to tap (unreliable for in-engine gesture dispatch). `dumpCompact` keeps elements with **any** addressable signal, not just clickable + text.
- **Platform-agnostic tool layer**. Tools talk to `DeviceController`. Adding iOS = implementing one adapter.
- **Cached UI tree on device (Android)**. Invalidated by accessibility events. Static screens serve from cache; typical tap latency ~30ms.
- **Strategy patterns throughout**. Step handlers, selector resolvers, storage backends, input-finding strategies. Adding a new type = drop in a new file, no core edits.
- **Zero external coupling**. adet is standalone. No workspace inheritance, no shared tsconfig, no hidden dependencies.

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
      "type": "stdio",
      "command": "node",
      "args": ["/absolute/path/to/adet/dist/index.js"],
      "env": {}
    }
  }
}
```

Restart Claude Code → `/mcp` lists `adet` with 19 tools. Prompt it:

> "List connected devices, pick the first, launch com.example.app, fill the login form with user@test.com / hunter2, tap Login, verify you land on the home screen."

---

## Troubleshooting

| Symptom                                                    | Fix                                                                                                                   |
| ---------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------- |
| `/health` returns `accessibilityConnected: false`          | Rebind via the shell snippet in Quick start §2.                                                                       |
| `get_ui_tree` returns empty + `currentActivity` empty      | Stale a11y binding after APK install. `launch_app` now preflights this and surfaces an actionable rebind hint.        |
| Input fill appends instead of replacing                    | `input_text` clears by default (`clearFirst: true`). If you see append behavior, ensure you're using `input_text`, not manual tap + type_via_keyboard chaining. |
| `tap({resourceId:"G01-05-01/2"})` reports NOT FOUND        | The Android native lookup requires `package:id/` prefix. `ResourceIdStrategy` has a walk fallback — should resolve automatically. If not, file an issue.       |
| `tap({text:"OK"})` fails on Android                        | Material buttons often set only `contentDesc`. `tap` auto-broadens across selector types — should succeed. If not, the element isn't in the current tree.     |
| `get_screenshot` returns 500                               | Device must be API 30+ for `AccessibilityService.takeScreenshot()`.                                                   |
| Foreground notification disappears                         | Battery optimization killed the service. Add adet to "Don't optimize" in battery settings.                            |
| iOS device returns "not implemented"                       | iOS is 🟡 TODO — approach not yet decided. See [docs/ios.md](./docs/ios.md).                                           |

---

## Contributing

See [CLAUDE.md](./CLAUDE.md) for architecture principles and extension patterns.

**iOS support is an open design question** — see [docs/ios.md](./docs/ios.md). Before writing code, read the candidate approaches, prototype your preferred option, and open a discussion with measurements + tradeoffs.

---

## License

Apache 2.0 — see [LICENSE](./LICENSE).
