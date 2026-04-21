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

- **Pure Developer** — installing `@atomyx/mcp` alongside a driver
  adapter, driving devices from an MCP client, scripting test flows.
- **QC Manager** — installing the test-management CLI, organizing
  test cases (when the test-management module ships).
- **Power User** — installing the Studio GUI for the visual
  experience (when the studio module ships).
- **CI Pipeline Engineer** — wiring Atomyx into headless test runs
  and result reporting.

## Planned content

When real user documentation lands, it slots in here:

- `getting-started.md` — install, first run, hello-world MCP session
- `install/` — per-platform install guides (macOS, Linux, Windows,
  Docker)
- `mcp-setup.md` — wiring Atomyx into MCP-compatible clients
- `tools/` — user-facing reference for each MCP tool (the agent's
  view, not the implementer's)
- `selectors.md` — selector patterns + priority broadening from a
  user perspective
- `device-setup/`
  - `android.md` — installing the Atomyx Android APK, granting
    accessibility, troubleshooting
  - `ios.md` — Xcode setup, code signing, real device provisioning
- `recipes/` — common test patterns (login, payment, scroll-to-
  element, etc.)
- `troubleshooting.md` — common errors with fixes
- `cli.md` — CLI subcommand reference

Currently empty pending real content. See
[`architecture.md`](../.claude/docs/architecture.md) for the
high-level contract and [`.claude/docs/`](../.claude/docs/) for the
contributor view.
