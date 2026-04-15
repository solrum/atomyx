# @atomyx/core-driver-cli

`atomyx-driver` — the command-line entry point for the Atomyx
core-driver module. Wires the framework, the iOS + Android
drivers, and the MCP server into a single binary.

## Install

```bash
npm install -g @atomyx/core-driver-cli
```

## Use

Start an MCP server that drives an iOS simulator:

```bash
atomyx-driver mcp --platform ios --kind simulator
```

iOS physical device:

```bash
atomyx-driver mcp --platform ios --kind device --device 00008101-...
```

Android device or emulator:

```bash
atomyx-driver mcp --platform android --device emulator-5554
```

The MCP server speaks the standard
`@modelcontextprotocol/sdk` stdio protocol. Wire it into Claude
Code, Cursor, Continue, or any other MCP client to drive a real
device through the framework's tool surface.

## Subcommands

| Command | Purpose |
|---|---|
| `mcp` | Start MCP stdio server (the primary use case) |
| `list-devices` | Enumerate connected Android devices |
| `version` | Print version |
| `help` | Print usage |

Run `atomyx-driver help` for full flag documentation.

## How it wires together

```ts
// What the binary does, distilled
import { Orchestra, SystemClock, ConsoleLogger } from "@atomyx/core-driver";
import { IosDriver } from "@atomyx/core-driver-ios";
import { createMcpServer } from "@atomyx/core-driver-mcp";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";

const driver = new IosDriver({ kind: "simulator", udid: "..." });
await driver.connect();

const orchestra = new Orchestra({
  driver,
  clock: new SystemClock(),
  logger: new ConsoleLogger("info"),
});

const server = createMcpServer({ orchestra });
await server.connect(new StdioServerTransport());
```

The CLI is a thin wrapper around this composition. Library
users (Synapse integration, Studio, custom MCP hosts) can
import the same packages directly and skip the CLI entirely.

## See also

- [`@atomyx/core-driver`](../core-driver) — framework primitives
- [`@atomyx/core-driver-mcp`](../core-driver-mcp) — MCP server library
- [`@atomyx/core-driver-ios`](../core-driver-ios) — iOS driver
- [`@atomyx/core-driver-android`](../core-driver-android) — Android driver
- [`.claude/docs/architecture.md`](../../.claude/docs/architecture.md) — opt-in modular ecosystem contract
