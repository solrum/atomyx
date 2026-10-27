# Atomyx

> AI agents driving real mobile apps: cross-platform, selector-first,
> open source.

![License](https://img.shields.io/badge/license-MIT-blue)
![Status](https://img.shields.io/badge/status-in%20development-orange)

Atomyx lets an AI agent, or a plain YAML script, drive an iOS or
Android app the way a human tester would: discovering screens, trying
flows, and reporting bugs, through one small, stable core runtime.
Device selection, selector resolution, scroll-into-view, and gesture
dispatch are handled for you, so the same script or agent session
works on both platforms.

By [Solrum](https://atomyx.dev). MIT.

## How it works

```
            +-----------+     +-----------+
AI agent -> | atomyx-mcp|     | atomyx CLI| <- YAML script or CI job
            +-----------+     +-----------+
                    \             /
                     v           v
                  +---------------+
                  |  core runtime  |
                  |  (driver port, |
                  |  script engine)|
                  +---------------+
                     /           \
                    v             v
          +----------------+  +----------------+
          | Android agent  |  |   iOS agent    |
          | (Kotlin APK)   |  | (XCUITest bundle)|
          +----------------+  +----------------+
```

One core runtime, one `Driver` port. `atomyx-mcp` exposes it as an MCP
tool surface for AI agents, and the `atomyx` CLI exposes it for scripts
and CI. A native agent per platform does the actual on-device work
(accessibility tree dump, tap, swipe, screenshot), speaking a small
wire protocol back to the core.

## What it feels like

```yaml
format: atomyx/v1
appId: com.android.settings
name: Settings smoke test
---
- launchApp
- tap: "Connections"
- waitFor: "Wi-Fi"
- screenshot: connections_screen
- back
```

One YAML script, same result on iOS and Android.

## Quick start

If more than one device is attached, run `export ANDROID_SERIAL=<serial>`
first. `adb devices` lists the serials.

```bash
git clone https://github.com/solrum/atomyx.git
cd atomyx
make build-core                    # builds core/bin/atomyx and atomyx-mcp
cd platforms/android-agent && ./gradlew :app:assembleDebug
adb install -r app/build/outputs/apk/debug/app-debug.apk
adb shell settings delete secure enabled_accessibility_services
adb shell settings put secure enabled_accessibility_services dev.atomyx.agent/dev.atomyx.agent.service.AtomyxAccessibilityService
adb shell am start-foreground-service -n dev.atomyx.agent/dev.atomyx.agent.control.AtomyxForegroundService
cd ../..
adb forward tcp:8765 tcp:8765
make ready-android                 # confirms the driver can reach the device
core/bin/atomyx devices            # confirm the device shows up
core/bin/atomyx run --file examples/test-login-flow.yml --platform android --device <serial>
```

For the iOS path and every prerequisite in full, see
[`docs/getting-started.md`](./docs/getting-started.md).

## Use with an AI agent (MCP)

Build the core runtime with `make build-core`, which produces
`core/bin/atomyx-mcp`, then point your MCP client at the binary:

```json
{
  "mcpServers": {
    "atomyx": { "type": "stdio", "command": "/absolute/path/to/atomyx/core/bin/atomyx-mcp" }
  }
}
```

Restart the client. See [`docs/mcp-tools.md`](./docs/mcp-tools.md) for
what each tool does.

## Documentation

| Audience | Entry point |
|---|---|
| End users (install, run, script) | [`docs/`](./docs/) |
| Contributors (architecture, internals, pitfalls) | [`.claude/docs/`](./.claude/docs/) |
| YAML script authors | [`docs/yml-script-reference.md`](./docs/yml-script-reference.md) |
| Device setup (Android and iOS) | [`docs/device-setup.md`](./docs/device-setup.md) |
| MCP tool behavior | [`docs/mcp-tools.md`](./docs/mcp-tools.md) |
| Known limitations and workarounds | [`docs/known-limitations.md`](./docs/known-limitations.md) |
| Module status and how to check it | [`docs/status.md`](./docs/status.md) |

A separate desktop IDE, Atomyx Studio, is under development and is
not covered by these docs.

## Contributing

Issues and pull requests are welcome. Start with
[`.claude/docs/development.md`](./.claude/docs/development.md) for
build, test, and extension workflows, and
[`.claude/docs/architecture.md`](./.claude/docs/architecture.md) for
the design contract.

## License

MIT. See [`LICENSE`](./LICENSE).
