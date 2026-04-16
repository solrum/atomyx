# Atomyx

> **AI agents that test mobile apps.** Your agent thinks in user actions — open the app, tap the button, type the email, verify the screen. Atomyx handles the platform.

```ts
await launch_app({ appId: "com.example.app" });
await tap({ selector: { text: "Login", role: "button" } });
await input_text({ selector: { hint: "Email" }, text: "user@test.com" });
```

Same code drives iOS and Android. By [Solrum](https://atomyx.dev) · Apache 2.0.

---

## Install

```bash
npm install -g @atomyx/core-driver-cli
```

Requires Node.js 20+. Platform prerequisites:

- **Android**: `adb` on PATH (Android platform-tools)
- **iOS simulator**: Xcode + booted simulator
- **iOS device**: Xcode + Apple Developer team + `libimobiledevice` (`brew install libimobiledevice`)

## Quick start

### 1. Start the MCP server

iOS simulator:

```bash
atomyx-driver mcp --platform ios --kind simulator
```

Android device or emulator:

```bash
atomyx-driver mcp --platform android --device emulator-5554
```

iOS physical device:

```bash
atomyx-driver mcp --platform ios --kind device --device <udid>
```

### 2. Connect from your MCP client

Add to your client's MCP config (Claude Code example):

```json
{
  "mcpServers": {
    "atomyx": {
      "command": "atomyx-driver",
      "args": ["mcp", "--platform", "ios", "--kind", "simulator"]
    }
  }
}
```

Restart the client → the Atomyx tools appear (`launch_app`, `tap`, `find_element`, `input_text`, `swipe`, `screenshot`, `get_ui_tree`, `wait_for_element`, `press_key`).

### 3. Drive a device

In a chat with your agent:

> Open com.example.app, find the Login button, tap it, then type user@test.com into the email field.

The agent calls the tools; Atomyx handles selector resolution, scroll-into-view, and obscurement checking automatically.

## Tools

| Tool | Purpose |
|---|---|
| `launch_app` | Bring an app to foreground |
| `get_ui_tree` | Snapshot the current screen as a flat element list |
| `find_element` | Resolve a selector to coordinates + metadata |
| `tap` | Tap by selector or coordinates |
| `input_text` | Type into a field by selector or coordinates |
| `swipe` | Directional or two-point swipe |
| `press_key` | Press back / home / enter / etc. |
| `screenshot` | PNG snapshot |
| `wait_for_element` | Polling wait with timeout |

## Status

| Platform | Status | Notes |
|---|---|---|
| iOS simulator | 🟢 Preview | Xcode 15+ |
| iOS physical device | 🟢 Preview | Requires Apple Developer team + iproxy |
| Android device + emulator | 🟢 Preview | API 26+, screenshots require API 30+ |
| Web (browser) | 🟡 Roadmap | — |
| Desktop | 🟡 Roadmap | — |

## Architecture

Atomyx ships as opt-in npm packages — install only what you need:

```
@atomyx/core-driver-cli         ← end-user binary (this README's install)
  └─ depends on:
     @atomyx/core-driver        ← framework primitives
     @atomyx/core-driver-ios    ← iOS driver
     @atomyx/core-driver-android   ← Android driver
     @atomyx/core-driver-mcp    ← MCP server
```

Library consumers can skip the CLI and import the packages directly. See [`.claude/docs/architecture.md`](./.claude/docs/architecture.md) for the full architectural contract.

## Documentation

- [`docs/`](./docs/) — user guides (install, MCP setup, recipes)
- [`.claude/docs/`](./.claude/docs/) — contributor + AI-agent reference

## License

Apache 2.0. See [LICENSE](./LICENSE).
