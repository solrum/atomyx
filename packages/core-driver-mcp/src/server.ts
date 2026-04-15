import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import {
  CallToolRequestSchema,
  ListToolsRequestSchema,
} from "@modelcontextprotocol/sdk/types.js";
import type { Orchestra, Logger } from "@atomyx/core-driver";
import { NoopLogger } from "@atomyx/core-driver";
import type { AnyToolDefinition, ToolContext } from "./tool-definition.js";
import { DEFAULT_TOOLS } from "./tools/index.js";
import { zodToJsonSchema } from "./zod-to-json-schema.js";

/**
 * `createMcpServer` — factory that returns a configured MCP
 * `Server` ready to be wired to a transport. The factory does
 * NOT start the transport itself — callers (e.g. `apps/cli`)
 * compose the server with `StdioServerTransport` or any other
 * MCP-supported transport.
 *
 * Architectural rules this factory enforces:
 *
 *   1. **Orchestra is the only mutator.** Every tool that
 *      changes device state goes through `ctx.orchestra`. No
 *      tool reaches around to call a Driver method directly.
 *      Future contributors who add tools must follow this — the
 *      `ToolContext` type doesn't expose a Driver field, so the
 *      type system enforces it.
 *
 *   2. **Tools are pluggable.** Pass `tools: [...customTools]`
 *      to ship a different surface than DEFAULT_TOOLS. Studio
 *      can include diagnostic/replay tools the standard MCP
 *      server doesn't expose; Synapse can omit Mode C explorer
 *      tools its UI handles directly.
 *
 *   3. **No global state.** Every server instance is independent
 *      — the factory returns a fresh `Server` each call and
 *      tools share the `ToolContext` only via constructor
 *      injection. Multiple parallel servers (e.g. in tests) work
 *      out of the box.
 *
 *   4. **Errors are structured, not panicked.** Tool failures
 *      become `{isError:true, content:[{type:"text", text:msg}]}`
 *      MCP responses. Only programming errors (malformed args
 *      that pass schema validation, internal invariant
 *      violations) propagate as exceptions and crash the request.
 */

export interface McpServerOptions {
  /** Compose with a Driver via `new Orchestra({driver, clock})`. */
  readonly orchestra: Orchestra;
  /**
   * Override the tool surface. Defaults to `DEFAULT_TOOLS` from
   * `./tools/index.ts` (~9 tools covering the core agent
   * workflow). Pass a different list to ship a custom surface.
   */
  readonly tools?: readonly AnyToolDefinition[];
  /**
   * Server-side logger. Defaults to `NoopLogger`. The CLI binary
   * typically passes a `ConsoleLogger("info")` so structured
   * tool dispatch lines hit stderr alongside the MCP traffic.
   */
  readonly logger?: Logger;
  /**
   * MCP server identification surfaced in the
   * `initialize` response. Defaults to `{name: "atomyx", version:
   * "0.1.0"}`. Override when embedding into Studio or Synapse so
   * the agent's UI shows the right product label.
   */
  readonly serverInfo?: {
    readonly name: string;
    readonly version: string;
  };
}

export function createMcpServer(opts: McpServerOptions): Server {
  const tools = opts.tools ?? DEFAULT_TOOLS;
  const logger = opts.logger ?? new NoopLogger();
  const ctx: ToolContext = {
    orchestra: opts.orchestra,
    logger,
  };

  // Build a name → tool index once. Tools are immutable for the
  // lifetime of the server instance.
  const byName = new Map<string, AnyToolDefinition>();
  for (const t of tools) {
    if (byName.has(t.name)) {
      throw new Error(
        `createMcpServer: duplicate tool name "${t.name}". Each tool must have a unique name.`,
      );
    }
    byName.set(t.name, t);
  }

  const server = new Server(
    opts.serverInfo ?? { name: "atomyx", version: "0.1.0" },
    { capabilities: { tools: {} } },
  );

  // tools/list — return JSON-Schema-converted tool descriptors.
  server.setRequestHandler(ListToolsRequestSchema, async () => ({
    tools: tools.map((t) => ({
      name: t.name,
      description: t.description,
      inputSchema: zodToJsonSchema(t.inputSchema),
    })),
  }));

  // tools/call — validate args via Zod, dispatch to handler,
  // wrap result in MCP content envelope. Errors become
  // structured isError responses, NOT thrown.
  server.setRequestHandler(CallToolRequestSchema, async (req) => {
    const tool = byName.get(req.params.name);
    if (!tool) {
      return {
        isError: true,
        content: [{ type: "text", text: `Unknown tool: ${req.params.name}` }],
      };
    }

    // Zod validates the raw args object. Validation errors return
    // a structured isError response with the issue message; the
    // agent can see what was wrong and retry.
    const parsed = tool.inputSchema.safeParse(req.params.arguments ?? {});
    if (!parsed.success) {
      return {
        isError: true,
        content: [
          {
            type: "text",
            text: `Invalid arguments for ${tool.name}: ${parsed.error.message}`,
          },
        ],
      };
    }

    const startedAt = Date.now();
    try {
      const result = await tool.execute(parsed.data, ctx);
      const durationMs = Date.now() - startedAt;
      logger.debug("tool.success", {
        tool: tool.name,
        durationMs,
      });
      return {
        content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
      };
    } catch (err) {
      const durationMs = Date.now() - startedAt;
      const message = err instanceof Error ? err.message : String(err);
      logger.warn("tool.error", {
        tool: tool.name,
        durationMs,
        error: message,
      });
      return {
        isError: true,
        content: [{ type: "text", text: message }],
      };
    }
  });

  return server;
}
