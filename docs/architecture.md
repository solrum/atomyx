# adet — Architecture reference

High-level map of the codebase. Read this when you need to locate a module
or understand how the layers fit together. For tool-specific signatures /
invariants, see [`tools.md`](./tools.md).

## Layers

1. **MCP server** (TypeScript, `src/`) — platform-agnostic. Exposes **19 MCP
   tools** to Claude / any MCP client. Talks to devices through a single
   `DeviceController` port.
2. **Android control plane** (Kotlin, `android/`) — standalone APK hosting an
   HTTP server on `127.0.0.1:8765`. Connected via `adb forward`.
3. **iOS control plane** (🟡 TODO) — bridge approach not yet decided. See
   [`ios.md`](./ios.md) and `ios/README.md`.

The tool layer, runner, explorer, and storage are **platform-agnostic**. They
talk to a `DeviceController` interface. Adding a new platform = implementing
one adapter.

## Repo map

```
adet/
├── src/                           # TypeScript MCP server — platform-agnostic
│   ├── runtime/adet-context.ts    # DI container — controller, history, results,
│   │                              # recordedActions, invalidateUiCache, lastToolName
│   ├── adapters/                  # Per-platform DeviceController implementations
│   │   ├── device-controller.port.ts   # 4 split interfaces (ISP):
│   │   │                               #   Inspector + Actor + AppManager + Lifecycle
│   │   │                               # PLATFORM-NEUTRAL (appId, ForegroundInfo,
│   │   │                               # Selector with iOS predicate/classChain)
│   │   ├── agent-direct.adapter.ts     # Android: HTTP client to adet APK via
│   │   │                               # adb forward; translates appId ↔ packageName
│   │   │                               # at the boundary
│   │   ├── ios-xctest.adapter.ts       # iOS: STUB. Candidate bridges documented in-file
│   │   └── device-router.ts            # select_device picks the right adapter
│   ├── runner/                    # Mode B — deterministic YAML spec runner (not wired)
│   │   └── steps/                 # Strategy registry: one handler per step type
│   ├── explorer/                  # Mode C — Claude API agent loop (not wired)
│   ├── storage/
│   │   └── test-case-storage.ts   # Strategy: LocalFile | EngineHttp | Composite
│   ├── tools/
│   │   ├── core/                  # Strategy classes + Tool base — SEE tools.md
│   │   │   ├── tool.ts                        # abstract Tool<TShape> base class
│   │   │   ├── selector-resolution-pipeline.ts   # priority broadening
│   │   │   ├── ime-geometric-guard.ts         # structural coord-in-IME check
│   │   │   ├── fuzzy-resource-matcher.ts      # 3-tier resourceId match
│   │   │   ├── ambiguity-detector.ts          # duplicate-token detection
│   │   │   ├── structural-input-finder.ts     # 4-strategy input chain
│   │   │   ├── transition-classifier.ts       # tap-and-wait failure classify
│   │   │   └── ui-tree-cache.ts               # 2s dedupe cache, shared across tools
│   │   ├── <name>.tool.ts          # class <Name>Tool extends Tool<...>
│   │   ├── trivial.tools.ts        # tiny tools batched in one file
│   │   ├── tool-factory.ts         # ToolFactory.registerTool(instance)
│   │   ├── tree-render.ts          # renderCompactLine + filter/sort helpers
│   │   ├── find-input.ts           # pure fns wrapped by StructuralInputFinder
│   │   ├── preflight.ts            # stale binding detection + rebind hints
│   │   ├── transition-diagnostics.ts   # pure fns wrapped by TransitionClassifier
│   │   ├── selector-quality.ts     # warn when text used but stable id exists
│   │   └── playbook-tools.ts       # get_playbook + add_case_study + get_case_studies
│   └── registry.ts                # buildToolRegistry(ctx) — constructs strategies
│                                   # + class tools (all 19 class-based)
│
├── android/                       # Android control plane — standalone Gradle project
│   └── app/src/main/java/dev/solrum/adet/agent/
│       ├── service/AdetAccessibilityService.kt   # Slim a11y service
│       ├── ui/MainActivity.kt                    # Launcher
│       └── control/
│           ├── HttpControlServer.kt      # Thin NanoHTTPD dispatcher
│           ├── AdetServices.kt           # DI container per a11y instance
│           ├── AdetForegroundService.kt  # Foreground service + notification
│           ├── UiTreeService.kt          # Event-invalidated tree cache;
│           │                              # dumpCompact keeps anything with a
│           │                              # stable signal
│           ├── GestureDispatcher.kt      # tap + longPress + inputText +
│           │                              # typeViaKeyboard (w/ on-screen-keys
│           │                              # fallback for custom Flutter keypads)
│           │                              # + clearFocusedInput + forceStopApp
│           ├── SelectorResolver.kt       # Strategy chain — fastest first
│           ├── router/                   # Route interface + per-endpoint classes
│           └── strategy/                 # ResolutionStrategy implementations
│                                          #  - ResourceIdStrategy has a WALK
│                                          #    FALLBACK for Flutter / Compose / RN
│                                          #    non-qualified ids.
│
├── ios/                           # iOS control plane (🟡 TODO — no code today)
│   └── README.md                  # Why the dir is empty + pointer to docs/ios.md
│
└── docs/
    ├── architecture.md            # This file
    ├── tools.md                   # Tool signatures + invariants
    ├── development.md             # Build / test / extension workflow
    ├── pitfalls.md                # Known traps to avoid
    └── ios.md                     # iOS open design questions
```

## The 19 tools

| Category  | Tools                                                                 |
| --------- | --------------------------------------------------------------------- |
| Device    | `list_devices`, `select_device`                                       |
| App       | `launch_app` (defaults `forceStop=true`, returns `inputs[]` + `initialTree`), `list_apps` |
| Screen    | `get_ui_tree`, `find_element` *(supports `keyword`, `nth`, `nthOfRole`, `inputField`, `all`)*, `get_screenshot` |
| Actions   | `tap` *(selector OR {x,y})*, `tap_and_wait_transition`, `input_text` *(selector OR {x,y})*, `swipe`, `press_key` |
| Wait      | `wait_for_element` *(supports `absent: true`)*                        |
| Run       | `start_run`, `finish_run`, `report_bug`                               |
| Guidance  | `get_playbook`, `add_case_study`, `get_case_studies`                  |

The tool surface was consolidated from ~40 to 19 because agents were confused
by overlapping intents. Before adding a tool, ask whether an existing tool can
absorb the new intent via a parameter.
