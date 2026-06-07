# Atomyx — Status tracker

## Purpose

For a new session to learn the current version, active branch, and
per-package test counts without re-exploring the codebase. Update
this file on release cuts and when test counts change materially.

This file does NOT describe features (see each package's README or
`development.md`), tool contracts (see `tools.md`), or platform
internals (see `ios.md`). It answers exactly: "what version, what
branch, how many tests, what's out of scope for now?"

## Current version

- **Released**: `v0.1.0` on `main`.
- **Active branch**: `feature/v0.1.0-foundation-1` (HEAD `c2860e9`).
  Carries the Studio foundation, `@atomyx/skills` distribution
  package, CLI `init` / `update-skills` subcommands, and MCP
  instructions hook.
- **Next milestone**: `v1.0.0` (sidecar-backed run pipeline, tool
  surface, script engine major release).

## Per-package test counts

| Package | Tests |
|---|---|
| `@atomyx/shared` | 22 |
| `@atomyx/core` | 25 |
| `@atomyx/driver` | 141 |
| `@atomyx/driver-wire` | 17 |
| `@atomyx/android-driver` | 51 |
| `@atomyx/ios-driver` | 47 |
| `@atomyx/mcp` | 90 |
| `@atomyx/script` | 123 |
| `@atomyx/cli` | 37 |
| `@atomyx/skills` | 6 |
| `@atomyx/studio` | 54 |
| **Total** | **613** (0 failures) |

Recompute the totals when a PR adds or removes tests. Keep this table
honest — a stale count is worse than no count.

## Packages

### `@atomyx/skills` (`packages/skills/`)

Skills distribution package at version `0.1.0`. A leaf package with
no workspace dependencies.

Public API:

- `copySkillsTo(targetDir, { overwrite })` — copies the bundled
  skill and agent files into `<targetDir>/.claude/`.
- `getInstalledVersion(targetDir)` — reads the version marker from
  an existing install.
- `currentVersion` — the package version string.
- `SKILL_FILES` — array of bundled skill markdown filenames.
- `AGENT_FILES` — array of bundled agent markdown filenames.
- `getContentRoot()` — absolute path to the bundled content
  directory.

Bundled skills (under `packages/skills/content/skills/`):

- `atomyx-test-loop.md`
- `atomyx-debug-failure.md`
- `atomyx-script-authoring.md`

Bundled agents (under `packages/skills/content/agents/`):

- `atomyx-explorer.md`
- `atomyx-replayer.md`

### `@atomyx/cli` subcommands — `skills` module

Two subcommands live at `packages/cli/src/skills/`:

- `atomyx init` — copies the `@atomyx/skills` bundle into
  `<cwd>/.claude/` (or `--target=<path>`). Non-destructive by
  default; `--force` to overwrite existing files.
- `atomyx update-skills` — version-checks the installed bundle
  against `currentVersion`; replaces in place when the versions
  differ.

The module follows the same `execute.ts` dispatcher + per-command
file shape as the `driver/` module in the CLI.

### `@atomyx/mcp` — instructions hook

The MCP server's `instructions` template (at
`packages/mcp/src/bin.ts`) includes a hook that tells the
downstream agent to load Atomyx workflow skills from
`.claude/skills/` when that directory is present before starting
a mobile testing task.

## Known limitations

- **iOS simulator proxy**: Flutter apps can't read system proxy
  settings on the simulator. Proxy capture testing requires a
  physical device.
- **iOS driver trust**: proxy ON requires `--ignore-hosts` for Apple
  domains. Initial developer-certificate trust requires proxy OFF
  (one-time).
- **`.xctrunner` suffix**: Xcode auto-appends to UI-testing bundles
  and cannot be removed.
- **USB tether only** for iOS physical devices — `iproxy` is USB-
  only, no Wi-Fi usbmux.
- **Cross-app iOS navigation** desyncs driver-tracked `currentApp`;
  see `ios.md`.

## Out of scope until after v1.0

- MCP-dependent Studio UI (device mirror, inspector, record mode).
- Downstream consumer packages (`@atomyx/test-mgmt`, `@atomyx/cloud`)
  — empty skeletons only.
- HTTP transport layer on any module.
- Changeset-based release pipeline.
- Pluggable remote `Storage` adapter.
- CI workflow verification on hosted iOS hardware.
- Windows + Linux Studio builds.
- Studio auto-update (plugin wired, `active: false`).
