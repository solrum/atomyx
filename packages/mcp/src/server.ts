import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import {
  CallToolRequestSchema,
  ListToolsRequestSchema,
  GetPromptRequestSchema,
  ListPromptsRequestSchema,
} from "@modelcontextprotocol/sdk/types.js";
import type { Logger, Storage, Clock } from "@atomyx/driver";
import {
  NoopLogger,
  FileStorage,
  RunStore,
  SystemClock,
} from "@atomyx/driver";
import type { AnyToolDefinition, ToolContext } from "./tool-definition.js";
import { isImageResult } from "./tool-result.js";
import { DEFAULT_TOOLS } from "./tools/index.js";
import type { PromptDefinition } from "./prompts/prompt-definition.js";
import { DEFAULT_PROMPTS } from "./prompts/index.js";
import { zodToJsonSchema } from "./zod-to-json-schema.js";
import { DeviceSession } from "./device-session.js";

/**
 * `createMcpServer` — factory that returns a configured MCP
 * `Server` ready to be wired to a transport. The factory does
 * NOT start the transport itself — callers compose the server
 * with `StdioServerTransport` or any other MCP-supported
 * transport.
 *
 * Architectural rules this factory enforces:
 *
 *   1. **DeviceSession is the only mutator.** Every tool that
 *      changes device state reads `ctx.session.current()` to
 *      get the active Orchestra. No tool reaches around to call
 *      a Driver method directly. `ToolContext` doesn't expose a
 *      Driver field, so the type system enforces it.
 *
 *   2. **Runtime device selection.** The server does NOT commit
 *      to a driver at construction time. Agents call
 *      `select_device(platform, id)` to bind a device mid-
 *      session; subsequent tool calls flow through that device's
 *      Orchestra. Calling `select_device` again disconnects the
 *      previous driver and wires a new one, so one process can
 *      drive any platform without a restart.
 *
 *   3. **Tools are pluggable.** Pass `tools: [...customTools]`
 *      to ship a different surface than DEFAULT_TOOLS.
 *
 *   4. **No global state.** Every server instance is independent
 *      — the factory returns a fresh `Server` + fresh
 *      `DeviceSession` each call; tools share the `ToolContext`
 *      only via constructor injection.
 *
 *   5. **Errors are structured, not panicked.** Tool failures
 *      become `{isError:true, content:[{type:"text", text:msg}]}`
 *      MCP responses. Only programming errors (malformed args
 *      that pass schema validation, internal invariant
 *      violations) propagate as exceptions and crash the request.
 */

export interface McpServerOptions {
  /**
   * Runtime device-selection container. The server holds a
   * reference; tools read `ctx.session.current()` to access the
   * currently-bound Orchestra. When a caller does not pass a
   * session, `createMcpServer` refuses to start — the binary
   * layer (`atomyx-mcp`) is responsible for building a session
   * with the right driver factories.
   */
  readonly session: DeviceSession;
  /**
   * Override the tool surface. Defaults to `DEFAULT_TOOLS` from
   * `./tools/index.ts` (~9 tools covering the core agent
   * workflow). Pass a different list to ship a custom surface.
   */
  readonly tools?: readonly AnyToolDefinition[];
  /**
   * Override the methodology prompt surface. Defaults to
   * `DEFAULT_PROMPTS` from `./prompts/index.ts` — 4 built-in
   * testing methodology templates (`atomyx/playbook`,
   * `atomyx/exploratory`, `atomyx/regression`, `atomyx/bug-repro`).
   * Pass a different list to ship a custom surface, or an empty
   * array to disable the `prompts/` capability.
   */
  readonly prompts?: readonly PromptDefinition[];
  /**
   * Server-side logger. Defaults to `NoopLogger`. The CLI binary
   * typically passes a `ConsoleLogger("info")` so structured
   * tool dispatch lines hit stderr alongside the MCP traffic.
   */
  readonly logger?: Logger;
  /**
   * Persistent storage for bug reports, case studies, and run
   * artifacts. Defaults to `FileStorage` rooted at `~/.atomyx`
   * (override via `ATOMYX_STORAGE_DIR` env var). Tools that
   * write persistent data (`report_bug`, `add_case_study`,
   * `finish_run`) go through this. In-process consumers can
   * inject a custom `Storage` impl that routes to their own
   * backend.
   */
  readonly storage?: Storage;
  /**
   * Run lifecycle store. Defaults to a fresh in-memory
   * `RunStore` per server instance. Tools `start_run`,
   * `finish_run`, `report_bug` share this state — the agent
   * can run multiple test sessions sequentially within a
   * single server process.
   */
  readonly runStore?: RunStore;
  /**
   * Time source for tools that need polling / timeouts / backoff.
   * Defaults to `SystemClock` (wall-clock). Tests pass `FakeClock`
   * to drive deterministic fast-forward — see
   * `tools/new-tools.test.ts` `tap_and_wait_transition` for the
   * pattern.
   */
  readonly clock?: Clock;
  /**
   * MCP server identification surfaced in the `initialize`
   * response. Defaults to `{name: "atomyx", version: "0.1.0"}`.
   * Override when embedding so the agent's UI shows the host
   * product's label.
   */
  readonly serverInfo?: {
    readonly name: string;
    readonly version: string;
  };
}

export function createMcpServer(opts: McpServerOptions): Server {
  const tools = opts.tools ?? DEFAULT_TOOLS;
  const prompts = opts.prompts ?? DEFAULT_PROMPTS;
  const logger = opts.logger ?? new NoopLogger();
  const storage = opts.storage ?? new FileStorage();
  const runStore = opts.runStore ?? new RunStore();
  const clock = opts.clock ?? new SystemClock();
  const ctx: ToolContext = {
    session: opts.session,
    logger,
    storage,
    runStore,
    clock,
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

  // Same for prompts — unique names, immutable registry.
  const promptsByName = new Map<string, PromptDefinition>();
  for (const p of prompts) {
    if (promptsByName.has(p.name)) {
      throw new Error(
        `createMcpServer: duplicate prompt name "${p.name}". Each prompt must have a unique name.`,
      );
    }
    promptsByName.set(p.name, p);
  }

  // Announce both `tools` and `prompts` capabilities so MCP
  // clients know to call prompts/list alongside tools/list.
  const capabilities: Record<string, Record<string, unknown>> = {
    tools: {},
  };
  if (prompts.length > 0) {
    capabilities.prompts = {};
  }

  const server = new Server(
    opts.serverInfo ?? { name: "atomyx", version: "0.1.0" },
    { capabilities },
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
    const toolTimeoutMs = Number(process.env.ATOMYX_TOOL_TIMEOUT_MS) || 30_000;
    try {
      const result = await Promise.race([
        tool.execute(parsed.data, ctx),
        new Promise<never>((_resolve, reject) =>
          setTimeout(
            () => reject(new Error(`tool "${tool.name}" timed out after ${toolTimeoutMs}ms`)),
            toolTimeoutMs,
          ),
        ),
      ]);
      const durationMs = Date.now() - startedAt;
      logger.debug("tool.success", {
        tool: tool.name,
        durationMs,
      });
      if (isImageResult(result)) {
        return {
          content: [
            {
              type: "image" as const,
              data: result.data,
              mimeType: result.mimeType,
            },
          ],
        };
      }
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

  // prompts/list — return methodology prompt metadata.
  // Only registered when the prompts list is non-empty, so
  // callers who pass `prompts: []` opt out entirely and the
  // client sees no `prompts` capability in the initialize
  // response.
  if (prompts.length > 0) {
    server.setRequestHandler(ListPromptsRequestSchema, async () => ({
      prompts: prompts.map((p) => ({
        name: p.name,
        description: p.description,
        arguments: (p.arguments ?? []).map((a) => ({
          name: a.name,
          description: a.description,
          required: a.required ?? false,
        })),
      })),
    }));

    // prompts/get — render a named prompt with the provided
    // arguments. Unknown name → MCP protocol error (thrown,
    // not isError — prompts have no `isError` envelope in the
    // response schema).
    server.setRequestHandler(GetPromptRequestSchema, async (req) => {
      const prompt = promptsByName.get(req.params.name);
      if (!prompt) {
        throw new Error(`Unknown prompt: ${req.params.name}`);
      }
      const args = (req.params.arguments ?? {}) as Record<string, string>;
      const messages = prompt.render(args);
      logger.debug("prompt.rendered", {
        prompt: prompt.name,
        argCount: Object.keys(args).length,
        messageCount: messages.length,
      });
      return {
        description: prompt.description,
        messages: messages.map((m) => ({
          role: m.role,
          content: m.content,
        })),
      };
    });
  }

  return server;
}
