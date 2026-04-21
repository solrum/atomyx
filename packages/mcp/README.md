# @atomyx/mcp

MCP server library for Atomyx. Exports a `createMcpServer({session})`
factory that returns a configured `@modelcontextprotocol/sdk`
`Server` instance ready to connect to any MCP transport (stdio,
HTTP, WebSocket, in-process).

This is a **library**, not a runnable binary. The `atomyx` CLI
(`@atomyx/cli`) is the canonical entry point that wires this
library into a `StdioServerTransport`. Library consumers import
`createMcpServer` directly and connect to a transport of their
choice.

## Tool surface (27 tools)

Grouped by category; every tool is a thin wrapper over an
`Orchestra` method in `@atomyx/driver`. See
[`.claude/docs/tools.md`](../../.claude/docs/tools.md) for the
per-tool contract.

| Category | Tools |
|---|---|
| Device lifecycle | `list_devices`, `select_device`, `disconnect_device` |
| App lifecycle | `launch_app`, `list_apps` |
| Tree inspection | `get_ui_tree`, `find_element`, `wait_for_element` |
| Selector actions | `tap`, `tap_and_wait_transition`, `input_text`, `swipe`, `press_key` |
| Media | `screenshot` |
| Script | `run_script` |
| Runs | `start_run`, `finish_run`, `list_runs`, `get_run`, `update_run_summary`, `delete_run` |
| Bugs | `report_bug`, `list_bugs`, `get_bug`, `delete_bug` |
| Case studies | `add_case_study`, `get_case_studies` |

No business logic lives in the tool files — tools compose the
`Orchestra` pipeline (scroll-into-view, obscurement, priority
broadening, observation-driven wait primitives) that lives in
`@atomyx/driver`.

## Usage

```ts
import { Orchestra, SystemClock, ConsoleLogger } from "@atomyx/driver";
import { IosDriver } from "@atomyx/ios-driver";
import { AndroidDriver } from "@atomyx/android-driver";
import { createMcpServer, DeviceSession } from "@atomyx/mcp";
import { StdioServerTransport } from
  "@modelcontextprotocol/sdk/server/stdio.js";

const session = new DeviceSession({
  clock: new SystemClock(),
  logger: new ConsoleLogger("info"),
  driverFactories: {
    ios: (id, opts) => new IosDriver({ kind: opts.kind ?? "simulator", udid: id }),
    android: (id) => new AndroidDriver({ serial: id }),
  },
});

const server = createMcpServer({ session });
await server.connect(new StdioServerTransport());
```

The server starts with no active device. The agent picks one via
the `list_devices` → `select_device` tool flow at runtime; the same
process can drive iOS, then Android, then iOS again without a
restart.

## Pluggable tool surface

The factory accepts a custom `tools` list to ship a different
surface than `DEFAULT_TOOLS`:

```ts
import { createMcpServer, DEFAULT_TOOLS } from "@atomyx/mcp";
import { customReplayTool } from "./my-tool.js";

const server = createMcpServer({
  session,
  tools: [...DEFAULT_TOOLS, customReplayTool],
});
```

Library consumers add diagnostic tools their MCP host needs or
replace the default surface with domain-specific tools entirely.
No fork of this package needed.

## Contract guarantees

- **Tools never reach the Driver directly.** The type system
  blocks it: `ToolContext` exposes only the `DeviceSession`, and
  tools go through `ctx.session.current()?.orchestra`.
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

- `@atomyx/driver` — `Orchestra`, `Driver` types, filter
  composition, observable-state helpers, wait primitives,
  MockDriver for tests.
- `@modelcontextprotocol/sdk` — the MCP protocol types + Server.
- `zod` — input schema validation.

## See also

- [`@atomyx/cli`](../cli) — the CLI that wires this library to stdio.
- [`.claude/docs/tools.md`](../../.claude/docs/tools.md) — tool
  implementation reference.
- [`.claude/docs/architecture.md`](../../.claude/docs/architecture.md)
  — opt-in modular ecosystem contract.
