import type { z } from "zod";
import type {
  Orchestra,
  Logger,
  Storage,
  RunStore,
  Clock,
} from "@atomyx/driver";
import type { DeviceSession } from "./device-session.js";

/**
 * Common context every MCP tool receives at execution time.
 * Built once per `createMcpServer({session, ...})` call and
 * passed into every tool's handler.
 *
 * ## Device access
 *
 * Tools that need device I/O read `ctx.session.current()`:
 *
 *   - Returns the active `{ platform, id, orchestra, driver }`
 *     when the agent has selected a device via `select_device`
 *   - Returns `null` when no device is bound — in that case the
 *     tool should return `{ok: false, reason: "no active
 *     device — call select_device first"}` instead of throwing
 *
 * Session-based access lets one MCP process drive multiple
 * devices in sequence, or start idle and bind a device at first
 * use. Tools never hold a driver reference directly — they go
 * through the session on each call so switching devices mid-
 * session is observable immediately.
 *
 * The `orchestraOrFail()` helper throws a consistent error when
 * a tool absolutely needs a device — tool handlers can call it
 * at the top and let the server's catch-all convert the throw
 * into an `{isError: true}` response. Prefer returning
 * `{ok: false, reason}` when the failure is agent-actionable
 * (agent can call select_device and retry), and use
 * `orchestraOrFail()` only for tools where the absence of a
 * device is a flat-out usage error.
 *
 * ## Non-device services
 *
 * `storage`, `runStore`, `clock`, and `logger` are host-side
 * services that never touch the device. They persist across
 * device switches within a single MCP session — a test run
 * started on Android can be finished on iOS, which is useful
 * for cross-platform regression flows.
 *
 * All services are provided by `createMcpServer` with sensible
 * defaults. Tool authors should not construct their own
 * instances — always use what the context provides.
 */
export interface ToolContext {
  readonly session: DeviceSession;
  readonly logger: Logger;
  readonly storage: Storage;
  readonly runStore: RunStore;
  readonly clock: Clock;
  /**
   * Per-call abort signal owned by the server's tool wrapper. The
   * server aborts this when the tool's own declared budget elapses
   * so the underlying driver call can tear down its in-flight
   * request instead of hanging.
   *
   * Tool handlers MUST forward this signal into every Orchestra /
   * Finder call as `{ signal: ctx.signal }`. Long-running internal
   * loops (polling, retrying) MUST check `ctx.signal.aborted`
   * between iterations and throw early when set.
   */
  readonly signal: AbortSignal;
}

/**
 * Helper: resolve the current Orchestra or throw an actionable
 * error. Use from a tool handler like:
 *
 *     const orchestra = orchestraOrFail(ctx);
 *     return orchestra.tap(selector);
 *
 * The thrown error message is safe to surface to the agent as-
 * is; `server.setRequestHandler(CallToolRequestSchema)` catches
 * and wraps it as `{isError: true, content: [{text: message}]}`.
 */
export function orchestraOrFail(ctx: ToolContext): Orchestra {
  const active = ctx.session.current();
  if (!active) {
    throw new Error(
      "no active device. Call `select_device` with a platform + id " +
        "from `list_devices` before driving the UI.",
    );
  }
  return active.orchestra;
}

/**
 * A single MCP tool definition. The shape mirrors what the
 * @modelcontextprotocol/sdk expects from `setRequestHandler` for
 * `ListToolsRequestSchema` + `CallToolRequestSchema`, but uses
 * Zod for input validation so we get type-safe `args` inside
 * `execute()` without a separate cast.
 *
 * Naming convention: tool `name` is `snake_case_with_underscores`
 * to match LLM tool-call ergonomics. Description is one or two
 * sentences explaining what the tool does; the LLM uses the
 * description to choose between tools, so keep it crisp and
 * action-oriented ("Tap an element matching the given selector",
 * NOT "This tool can be used to tap on elements...").
 */
export interface ToolDefinition<TArgs, TResult> {
  readonly name: string;
  readonly description: string;
  readonly inputSchema: z.ZodType<TArgs>;
  execute(args: TArgs, ctx: ToolContext): Promise<TResult>;
}

/**
 * Helper to declare a tool with full type inference. The Zod
 * schema's inferred type becomes `args` in the execute callback
 * automatically — no manual generic on `ToolDefinition` needed.
 *
 *     export const tapTool = defineTool({
 *       name: "tap",
 *       description: "Tap an element matching a selector.",
 *       inputSchema: TapArgsSchema,
 *       async execute(args, ctx) {
 *         return ctx.orchestra.tap(args.selector);
 *       },
 *     });
 */
export function defineTool<TSchema extends z.ZodType<unknown>, TResult>(def: {
  name: string;
  description: string;
  inputSchema: TSchema;
  execute(args: z.infer<TSchema>, ctx: ToolContext): Promise<TResult>;
}): ToolDefinition<z.infer<TSchema>, TResult> {
  return def as ToolDefinition<z.infer<TSchema>, TResult>;
}

/**
 * The "any tool" type used by the registry — allows storing
 * tools with different arg/result shapes in a single map without
 * losing the call signature.
 */
export type AnyToolDefinition = ToolDefinition<unknown, unknown>;
