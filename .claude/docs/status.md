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
- **Active branch**: `feature/ios-hardening`.
- **Next milestone**: `v1.0.0` (MCP + tools + script engine major
  release).

## Per-package test counts

| Package | Tests |
|---|---|
| `@atomyx/core` | 25 |
| `@atomyx/driver` | 139 |
| `@atomyx/driver-wire` | 17 |
| `@atomyx/android-driver` | 39 |
| `@atomyx/ios-driver` | 47 |
| `@atomyx/mcp` | 87 |
| `@atomyx/script` | 123 |
| `@atomyx/cli` | 20 |
| **Total** | **497** (0 failures) |

Recompute the totals when a PR adds or removes tests. Keep this table
honest — a stale count is worse than no count.

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

- Downstream consumer packages (`@atomyx/test-mgmt`, `@atomyx/studio`,
  `@atomyx/cloud`) — empty skeletons only.
- HTTP transport layer on any module.
- Changeset-based release pipeline.
- Pluggable remote `Storage` adapter.
- CI workflow verification in hosted iOS hardware.
