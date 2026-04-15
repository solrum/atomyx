# @atomyx/core-driver-mcp

MCP server library for the Atomyx core-driver module. Exports a
`createMcpServer({orchestra})` factory that returns a configured
`@modelcontextprotocol/sdk` `Server` instance ready to connect to
any MCP transport (stdio, HTTP, WebSocket, in-process).

This is a **library**, not a runnable binary. The `atomyx-driver`
CLI in `@atomyx/core-driver-cli` is the canonical entry point that
wires this library into a `StdioServerTransport`. Library consumers
(Synapse integration, Studio, custom MCP hosts) import
`createMcpServer` directly and connect to a transport of their
choice.

## Tool surface (9 tools)

| Tool | Purpose |
|---|---|
| `launch_app` | Bring an app to foreground |
| `get_ui_tree` | Snapshot the current screen as a flat element list |
| `find_element` | Resolve a selector to coordinates + metadata |
| `tap` | Tap by selector or coordinates |
| `input_text` | Type into a field by selector or coordinates |
| `swipe` | Directional or two-point swipe |
| `press_key` | Press back / home / enter / tab / escape / delete |
| `screenshot` | PNG snapshot (base64) |
| `wait_for_element` | Polling wait with timeout |

All tools are thin wrappers over `Orchestra` methods from
`@atomyx/core-driver`. No business logic lives in the tool files —
see [`.claude/docs/tools.md`](../../.claude/docs/tools.md) for the
contract.

## Usage

```ts
import { Orchestra, SystemClock, ConsoleLogger } from "@atomyx/core-driver";
import { IosDriver } from "@atomyx/core-driver-ios";
import { createMcpServer } from "@atomyx/core-driver-mcp";
import { StdioServerTransport } from
  "@modelcontextprotocol/sdk/server/stdio.js";

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

That's a complete MCP server. Swap `IosDriver` for `AndroidDriver`
to drive Android with zero other code changes.

## Pluggable tool surface

The factory accepts a custom `tools` list to ship a different
surface than `DEFAULT_TOOLS`:

```ts
import { createMcpServer, DEFAULT_TOOLS } from "@atomyx/core-driver-mcp";
import { customReplayTool } from "./my-tool.js";

const server = createMcpServer({
  orchestra,
  tools: [...DEFAULT_TOOLS, customReplayTool],
});
```

Studio can add diagnostic tools the stdio MCP server doesn't ship;
Synapse can replace the default tool surface with its own
test-management-specific tools. No fork of this package needed.

## Contract guarantees

- **`Orchestra` is the only mutator in `ToolContext`.** No tool
  can reach around and call a `Driver` method directly — the
  type system (`ToolContext` has no `driver` field) blocks it.
- **No global state.** Each `createMcpServer` call returns a
  fresh server. Parallel instances work out of the box (used
  heavily by tests).
- **Errors are structured.** Tool handler exceptions become
  `{ isError: true, content: [{ type: "text", text: msg }] }`
  MCP responses, never a server crash.
- **Tool list is explicit.** Adding a file to `src/tools/`
  requires editing `src/tools/index.ts` to ship it. Prevents
  accidental surface growth.

## Adding a tool

See [`.claude/docs/tools.md`](../../.claude/docs/tools.md) for the
full template. Short form:

1. Create `src/tools/<name>.tool.ts` using `defineTool`.
2. Append to `DEFAULT_TOOLS` in `src/tools/index.ts`.
3. Add a test to `src/server.test.ts`.

## Dependencies

- `@atomyx/core-driver` — `Orchestra`, `Driver` types, filter
  composition, MockDriver for tests
- `@modelcontextprotocol/sdk` — the MCP protocol types + Server
- `zod` — input schema validation

## See also

- [`@atomyx/core-driver-cli`](../core-driver-cli) — the CLI that
  wires this library to stdio
- [`.claude/docs/tools.md`](../../.claude/docs/tools.md) — tool
  implementation reference
- [`.claude/docs/architecture.md`](../../.claude/docs/architecture.md) — opt-in modular ecosystem contract
