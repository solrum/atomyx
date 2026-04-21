# Changelog

All notable changes to Atomyx will be documented in this file.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
This project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Observation-driven input synchronization across the framework —
  Orchestra `inputText` and `tap` replace sleep-based waits with
  driver-side self-synchronization and keyboard-gated retries.
- `@atomyx/driver/state/` — free functions that derive focused-node
  and keyboard state from any canonical `TreeNode`.
- `@atomyx/driver/waits/` — observation-driven wait primitives
  (`waitUntil`, `waitForFocus`, `waitForText`, `waitForInputReady`,
  `waitForInputCommitted`, `waitForKeyboard`, `waitForTreeStable`).
- `Driver.hideKeyboard()` + `canHideKeyboard` capability.
- iOS `HideKeyboardCommand` wire command.
- iOS `ClearFocusedInputCommand` ⌘A + ⌫ fast path with exact-length
  delete-loop fallback that reads the focused value length from
  `app.snapshot()` so the loop is bounded by real content.
- `TreeNode.focused` boolean (drivers emit it from their platform's
  focus signal) and `ext:isIme` attribute marker (Android IME root).
- Script `handle` command polls `when` conditions up to `timeout`
  (default 5 s) so scripts no longer need `sleep:` pads before it.
- Selector ranking in `compileSelector`: candidates scored by
  `clickable + focused`; `nth` overrides to preserve document order.
- `.claude/rules/comments.md` — repo commenting rules.

### Changed
- Script `stepDelay` default 500 → 0. Observation-driven actions
  remove the need for fixed inter-step padding.
- `examples/test-login-flow.yml` — removed explicit `sleep:` pads
  around `handle`; polling handles the transitions.
- iOS test app bundle id: `com.example.android` →
  `dev.atomyx.demo` (matches the script `appId`).

### Fixed
- Comprehensive comment audit across every package and platform
  agent: removed diff-narration ("used to", "replaces", "legacy"),
  retired product names, milestone labels, and magic numbers. See
  `.claude/rules/comments.md` for the rules applied.
- `.claude/docs/` synced with current implementation (tool count,
  test counts, new subsystems, iOS command surface).

## [0.1.0] — 2026-04-16

### Added
- **`@atomyx/core`** — generic infra: Clock, Logger, Storage, Sessions.
- **`@atomyx/driver`** — framework core: `Driver` port, `Orchestra`
  command layer, `TreeNode` canonical shape, filter composition,
  `Finder`, `ScrollController`, obscurement detection,
  `TransitionDiagnostics`, `MockDriver` testing kit.
- **`@atomyx/driver-wire`** — Zod wire-protocol schemas.
- **`@atomyx/android-driver`** — full `Driver` implementation over
  the Kotlin APK HTTP API, ADB device management, tree normalization.
- **`@atomyx/ios-driver`** — full `Driver` implementation: TCP JSON
  protocol, `iproxy` USB tunneling, `XctestLauncher`
  auto-build/spawn, crash recovery.
- **`@atomyx/mcp`** — MCP server with 27 tools, `DeviceSession`,
  device discovery, methodology prompts.
- **`@atomyx/cli`** — `atomyx` binary with `run --file`,
  `list-devices --json`, `mcp` subcommand.
- **`@atomyx/script`** — YAML test engine with parser, runner,
  and 17 commands.
- **Android control plane** (`platforms/android-agent/`) — Kotlin
  accessibility service, HTTP server, gesture dispatch, UI tree dump.
- **iOS control plane** (`platforms/ios-agent/`) — Swift XCUITest
  runner, TCP server, press-key strategy chain.

[Unreleased]: https://github.com/nickvnlr/atomyx/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/nickvnlr/atomyx/releases/tag/v0.1.0
