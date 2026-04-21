# Atomyx

> AI agents driving real mobile apps — cross-platform, selector-first,
> open source.

![License](https://img.shields.io/badge/license-Apache%202.0-blue)
![Status](https://img.shields.io/badge/status-pre--release-orange)
![Node](https://img.shields.io/badge/node-%E2%89%A5%2020-brightgreen)

Atomyx lets an AI agent drive iOS and Android apps the way a human QA
would — discover screens, try flows, report bugs — through a small,
stable set of high-intent tools. The same agent code drives both
platforms; device selection, selector resolution, scroll-into-view,
and z-order obscurement checking are handled for you.

By [Solrum](https://atomyx.dev) · Apache 2.0.

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

One YAML, same script for iOS and Android. An agent can also call the
same underlying primitives through the MCP tool surface — the two
front-ends share one `Driver` port.

## Status

Pre-1.0 (current: `v0.1.0`).

### Platforms

| OS | State |
|---|---|
| Android (device + emulator) | Preview |
| iOS (simulator + physical device) | Preview |
| Web (browser) | Planned |

### App UI frameworks

| Framework | State |
|---|---|
| Native Android (Views + Jetpack Compose) | Preview |
| Native iOS — UIKit | Preview |
| Native iOS — SwiftUI | Preview (verified against Apple system apps only; custom SwiftUI apps untested) |
| Flutter | Preview — known edge cases (custom keypads, unreliable `clickable` flag) |
| React Native | Untested — bug reports welcome |

### Features

| Feature | State |
|---|---|
| MCP tool surface (27 tools) | First release — `@atomyx/mcp` |
| YML script engine (17 commands) | Preview — `@atomyx/script` |
| API capture (mitmproxy integration) | Preview |
| Unified CLI binary (`atomyx`) | Planned |
| Studio (GUI) | Planned |

## Getting started

Atomyx ships as a set of opt-in npm packages. The first published
module is `@atomyx/mcp` — the MCP server that exposes the 27-tool
surface to any MCP-compatible client (Claude Code, Cursor, Continue,
custom agents).

Build from source while pre-registry:

```bash
git clone https://github.com/solrum/atomyx.git
cd atomyx && npm install && npm run build
```

Wire the built `@atomyx/mcp` binary into your MCP client's config
(`.mcp.json` or equivalent) with a driver kind (`ios` or `android`)
and device selector. Device prerequisites are covered in the
contributor docs under [`.claude/docs/`](./.claude/docs/).

## Documentation

| Audience | Entry point |
|---|---|
| End users (install, run, script) | [`docs/`](./docs/) |
| Contributors (architecture, internals, pitfalls) | [`.claude/docs/`](./.claude/docs/) |

## Contributing

Issues and PRs welcome. Start with
[`.claude/docs/development.md`](./.claude/docs/development.md) for
build, test, and extension workflows; and
[`.claude/docs/architecture.md`](./.claude/docs/architecture.md) for
the design contract.

## License

Apache 2.0. See [`LICENSE`](./LICENSE).
