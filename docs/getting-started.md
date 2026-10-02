# Getting started with Atomyx

## Purpose

For a new end user: from a fresh clone to an AI agent driving your
phone, or a YAML script running against a device.

This guide covers building the core runtime, MCP client setup, a
first smoke run, and where to go next. It does not cover device
prerequisites, see [`device-setup.md`](./device-setup.md), or the
full YAML command reference, see
[`yml-script-reference.md`](./yml-script-reference.md).

## Prerequisites

- **Go 1.25+** to build the core runtime. An older Go version also
  works: with `GOTOOLCHAIN=auto`, the default, Go downloads the 1.25
  toolchain on first build, which needs network access.
- **Git**.
- For Android: `adb` on `PATH` (Android platform-tools) and a JDK
  for Gradle.
- For iOS simulator or physical device: **Xcode 15+** and
  **xcodegen** (`brew install xcodegen`).
- For iOS simulator: at least one booted simulator.
- For iOS physical device: an **Apple Developer team** and
  `libimobiledevice` (`brew install libimobiledevice`).

## 1. Build the core runtime

```bash
git clone https://github.com/solrum/atomyx.git
cd atomyx
make build-core
```

This produces `core/bin/atomyx` and `core/bin/atomyx-mcp`, along with
a sidecar binary and two schema generators. Add `core/bin` to your
`PATH`, or reference the binaries by their full path in the steps
below.

Full build and test commands, including how to work on the core
runtime itself, are in
[`../.claude/docs/development.md`](../.claude/docs/development.md).

## 2. Set up the MCP server in your agent

Atomyx exposes its capabilities through a stdio MCP server, the
`atomyx-mcp` binary. The tool surface groups into device and app
lifecycle, UI inspection, interaction, run and bug reporting, case
studies, and script execution. See
[`mcp-tools.md`](./mcp-tools.md) for what each tool does and how it
behaves.

Add to your MCP client config (Claude Desktop, Claude Code, Cursor,
or similar):

```json
{
  "mcpServers": {
    "atomyx": {
      "type": "stdio",
      "command": "/absolute/path/to/atomyx/core/bin/atomyx-mcp",
      "args": []
    }
  }
}
```

Restart the client. Atomyx tools should appear, so try listing them
or ask the agent: "what atomyx tools do you have?"

## 3. Prepare a device

Bring up at least one device before asking the agent to drive it, or
before running a script.

**Android**:

If more than one device is attached, run `export ANDROID_SERIAL=<serial>`
first. `adb devices` lists the serials. `core/bin/atomyx` commands take
the device explicitly through `--device` instead.

```bash
adb devices              # confirm the device shows up
adb shell settings delete secure enabled_accessibility_services
adb shell settings put secure enabled_accessibility_services dev.atomyx.agent/dev.atomyx.agent.service.AtomyxAccessibilityService
adb shell am start-foreground-service -n dev.atomyx.agent/dev.atomyx.agent.control.AtomyxForegroundService
make ready-android       # confirms the driver can reach the device
```

**iOS simulator**:

```bash
xcrun simctl list devices booted
xcrun simctl boot "iPhone 16 Pro"   # if none are booted
make ready-ios-sim                  # starts the driver and keeps running
```

The Xcode project and the test bundle are set up automatically on
first run, so the first `make ready-ios-sim` takes noticeably longer
than later ones. Once both are in place, a later run with nothing
changed skips straight to serving.

`make ready-ios-sim` builds the driver and starts serving in the
foreground. It does not return while it serves, so leave it running
in its own terminal. From another terminal, confirm the driver is
reachable:

```bash
atomyx doctor --platform ios
```

Without `--device`, the command targets the device of the runner you are
serving on its default port, so only one device is involved. With
`--device <UDID>`, the CLI starts a runner of its own instead, see
step 5.

Stop the driver with Ctrl-C in the terminal running
`make ready-ios-sim`. Stopping only the `make` process itself, for
example by killing its process id directly instead of sending Ctrl-C
to the terminal, can leave the underlying test runner still listening
on its port. Confirm nothing is left listening, and stop it directly
if something is:

```bash
lsof -ti tcp:22087
kill <pid from the command above>
```

**iOS physical device**:

```bash
cp platforms/ios-agent/device.env.example platforms/ios-agent/device.env
$EDITOR platforms/ios-agent/device.env   # fill in DEVICE_UDID and DEV_TEAM
make ready-ios                      # starts the driver and keeps running
```

`make ready-ios` behaves the same way: the project and the test
bundle are set up automatically on first run, and it does not return
while the driver serves. Leave it running in its own terminal, then
confirm readiness the same way:

```bash
atomyx doctor --platform ios
```

Without `--device`, the command targets the device of the runner you are
serving on its default port, so only one device is involved. With
`--device <UDID>`, the CLI starts a runner of its own instead, see
step 5.

Stop it with Ctrl-C in that terminal, the same way as the simulator.

Full setup details, troubleshooting, and proxy capture are in
[`device-setup.md`](./device-setup.md).

## 4. First smoke: ask the agent

In a chat with your MCP-connected agent:

> Drive my device: list the devices you see, pick the one that says
> "android", launch the Settings app, take a screenshot, and describe
> what's on screen.

The agent calls a device tool, then an app-launch tool, then a
screenshot tool. You see the screenshot in the response.

## 5. Run a YAML script directly

Scripts run without an agent, which is useful for CI or regression
runs.

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

For iOS with `--device`, `atomyx` starts the driver itself and stops it
when the command ends, so you do not need `make ready-ios-sim` or
`make ready-ios` for it. Without `--device` it does not start one. It asks
the driver you serve yourself on the default port, 22087, which device it
belongs to, and runs against that device. A driver that names no device
is taken to serve the one connected iPhone. When no driver answers, or
several iPhones are connected, the command fails with DEVICE_NOT_FOUND,
so serve the driver or pass `--device`.

The first launch on a machine compiles the driver, which can take
several minutes, and progress lines are written to stderr. The compile
happens before the command takes the device, and it is bounded at 300
seconds. A compile that outlasts that fails with `RUNNER_BUILD_TIMEOUT`.

Driving a physical iPhone this way needs signing settings in the
environment, because the `atomyx` binary does not read
`platforms/ios-agent/device.env`:

```bash
export ATOMYX_DEV_TEAM=<your Apple development team id>
export ATOMYX_IOS_DRIVER_DIR=/absolute/path/to/atomyx/platforms/ios-agent
```

`ATOMYX_IOS_DRIVER_DIR` is needed only when the binary does not run from
inside the checkout. The same variables apply to `atomyx-mcp`.

Only one Atomyx process can drive a device at a time. A second command
against a device that is already in use fails with `DEVICE_HELD` and names
the process holding it. `atomyx doctor` reports the holder and exits `0`
instead of failing. See [`cli.md`](./cli.md) for the details.

To check that a device is reachable and healthy without running a
script, use `atomyx doctor --platform android --device <serial>`.

To look at a device's live UI tree and test selectors in a browser, run
`atomyx inspect --platform android --device <serial>` and open the URL it
prints. See [`cli.md`](./cli.md) for the idle release and Ctrl-C behavior.

Scripts can also express any W3C-style pointer gesture, including
long-press, drag, press-and-drag, and multi-finger pinch, through the
`pointer` command:

```yaml
- pointer:
    touches:
      - actions:
          - down: "Item A"
          - wait: 800
          - move: "Drop zone"
          - up
```

See [`yml-script-reference.md`](./yml-script-reference.md) for the
full form, including multiple concurrent touches, pressure, and
validation rules, and [`pointer.md`](./pointer.md) for a dedicated
gesture-authoring guide.

## 6. Run a suite of scripts

`atomyx suite` discovers and runs many scripts in one pass, selected
by tag or glob:

```bash
atomyx suite --tag smoke --platform android --device <serial>
```

Each script in a suite runs in its own clean execution context: `env`
variables and `capture` results from one script never leak into the
next. See [`yml-script-reference.md`](./yml-script-reference.md) for
scenario files that compose multiple scripts explicitly.

## Where to go next

| Want to | Read |
|---|---|
| Get a demo app to try scripts against | [`../examples/README.md`](../examples/README.md) |
| Write more complex flows: waiting, branching, API capture | [`yml-script-reference.md`](./yml-script-reference.md) |
| Every `atomyx` command and flag | [`cli.md`](./cli.md) |
| Troubleshoot device setup | [`device-setup.md`](./device-setup.md) |
| Understand the architecture | [`../.claude/docs/architecture.md`](../.claude/docs/architecture.md) |
| Extend the tool surface | [`../.claude/docs/tools.md`](../.claude/docs/tools.md) |
| Learn what traps the team already hit | [`../.claude/docs/pitfalls.md`](../.claude/docs/pitfalls.md) |
