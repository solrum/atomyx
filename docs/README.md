# Atomyx — User Documentation

This directory holds **end-user documentation** for the Atomyx
framework: getting started, installation, MCP setup, tool usage,
configuration, and integration guides.

**Contributor / implementation documentation lives in
[`.claude/docs/`](../.claude/docs/)** — that includes the
internal architecture, contributor workflow, iOS implementation
deep dive, known pitfalls, and tool implementation reference.
The split exists so end users opening `docs/` see only the
material relevant to USING Atomyx, while contributors and AI
agents working ON Atomyx find the deeper context in `.claude/`.

## Personas served by this directory

- **Pure Developer** — installing `@atomyx/core-driver-cli`,
  driving devices via MCP, scripting test flows.
- **QC Manager** — installing `@atomyx/test-mgmt-cli`,
  organizing test cases (when test-mgmt module ships).
- **Power User** — installing `@atomyx/studio` for the GUI
  experience (when studio module ships).
- **CI Pipeline Engineer** — wiring Atomyx into headless test
  runs and result reporting.

## Planned content

When real user documentation lands, it slots in here:

- `getting-started.md` — install, first run, hello-world MCP
  session
- `install/` — per-platform install guides (macOS, Linux,
  Windows, Docker)
- `mcp-setup.md` — wiring Atomyx into Claude Code, Cursor,
  Continue, or other MCP clients
- `tools/` — user-facing reference for each MCP tool (the
  agent's view, not the implementer's)
- `selectors.md` — selector patterns + priority broadening
  from a user perspective
- `device-setup/`
  - `android.md` — installing the Atomyx Android APK,
    granting accessibility, troubleshooting
  - `ios.md` — Xcode setup, code signing, real device
    provisioning
- `recipes/` — common test patterns (login flow, payment,
  scroll-to-element, etc.)
- `troubleshooting.md` — common errors with fixes
- `cli.md` — `atomyx-driver` command reference

Currently empty pending real content. See
[architecture.md](../.claude/docs/architecture.md) for the high-level
contract and [`.claude/docs/`](../.claude/docs/) for the
contributor view.
