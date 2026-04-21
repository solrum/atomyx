# Getting started with Atomyx

## Purpose

For a new end user: from fresh clone to an AI agent driving your
phone, or running a YAML script against a device.

This guide covers: install from source, MCP client setup, first
smoke run, and where to go next. It does NOT cover device prereqs
(see [`device-setup.md`](./device-setup.md)) or the full YAML command
reference (see [`yml-script-reference.md`](./yml-script-reference.md)).

## Prerequisites

- **Node.js 20+** and `npm`.
- **Git**.
- For Android: `adb` on `PATH` (Android platform-tools).
- For iOS simulator: **Xcode 15+** and at least one booted simulator.
- For iOS physical device: an **Apple Developer team** and
  `libimobiledevice` (`brew install libimobiledevice`).

## 1. Build from source

Atomyx is pre-release (`v0.1.0`) and not yet published to any
registry. Build locally:

```bash
git clone https://github.com/solrum/atomyx.git
cd atomyx
npm install
for d in core driver driver-wire android-driver ios-driver script mcp cli; do
  (cd packages/$d && npx tsc)
done
```

That produces the `atomyx` CLI binary (from `@atomyx/cli`) and the
`atomyx-mcp` binary (from `@atomyx/mcp`) in the workspace
`node_modules/.bin/`. See
[`../.claude/docs/development.md`](../.claude/docs/development.md)
for per-package test commands and an explanation of the build order.

Link them for convenience (optional):

```bash
npm link --workspace @atomyx/cli --workspace @atomyx/mcp
```

After this, `atomyx` and `atomyx-mcp` are on your `PATH`.

## 2. Set up the MCP server in your agent

Atomyx exposes 27 tools through a stdio MCP server. Today the server
ships as a separate binary `atomyx-mcp`; in v1.0 it will also be
reachable as `atomyx mcp`.

Add to your MCP client config (Claude Desktop, Claude Code, Cursor,
etc.):

```json
{
  "mcpServers": {
    "atomyx": {
      "command": "atomyx-mcp"
    }
  }
}
```

If `atomyx-mcp` is not on `PATH`, use the absolute path from the
build step:

```json
{
  "mcpServers": {
    "atomyx": {
      "command": "/absolute/path/to/atomyx/node_modules/.bin/atomyx-mcp"
    }
  }
}
```

Restart the client. Atomyx tools should appear — try listing them or
ask the agent: "what atomyx tools do you have?"

## 3. Prepare a device

Bring up at least one device before asking the agent to drive it.

**Android**:

```bash
adb devices              # confirm device shows up
make ready-android       # install the Kotlin agent APK + enable a11y
```

**iOS simulator**:

```bash
xcrun simctl list devices booted
xcrun simctl boot "iPhone 16 Pro"   # if none are booted
make ready-ios-sim
```

**iOS physical device**:

```bash
cp platforms/ios-agent/device.env.example platforms/ios-agent/device.env
$EDITOR platforms/ios-agent/device.env   # fill in DEVICE_UDID and DEV_TEAM
make ready-ios
```

Full setup details, troubleshooting, and proxy capture are in
[`device-setup.md`](./device-setup.md).

## 4. First smoke — ask the agent

In a chat with your MCP-connected agent:

> Run the Atomyx agent — list the devices you see, pick the one
> that says "android", launch the Settings app, take a screenshot,
> and describe what's on screen.

The agent should call `list_devices`, `select_device`, `launch_app`,
then `screenshot`. You'll see the screenshot in the response.

## 5. Run a YAML script directly

Scripts run without an agent — useful for CI or regression runs.

Create `test.yml`:

```yaml
format: atomyx/v1
appId: com.android.settings
name: Settings smoke
---
- launchApp
- tap: "Connections"
- waitFor: "Wi-Fi"
- screenshot: connections_screen
- back
```

Run it:

```bash
atomyx run --file test.yml --platform android --device <serial>
```

`<serial>` comes from `atomyx devices` or `adb devices`. For iOS use
`--platform ios --device <UDID>`.

Scripts can also express any W3C-style pointer gesture —
long-press, drag, press-and-drag, multi-finger pinch — via the
`pointer:` command:

```yaml
- pointer:
    actions:
      - down: "Item A"
      - wait: 800
      - move: "Drop zone"
      - up
```

See [`yml-script-reference.md`](./yml-script-reference.md) for
the full form (multi-pointer, pressure, validation rules).

## Where to go next

| Want to… | Read |
|---|---|
| Write more complex flows (wait, branch, API capture) | [`yml-script-reference.md`](./yml-script-reference.md) |
| Troubleshoot device setup | [`device-setup.md`](./device-setup.md) |
| Understand the architecture | [`../.claude/docs/architecture.md`](../.claude/docs/architecture.md) |
| Extend the tool surface | [`../.claude/docs/tools.md`](../.claude/docs/tools.md) |
| Learn what traps the team already hit | [`../.claude/docs/pitfalls.md`](../.claude/docs/pitfalls.md) |
