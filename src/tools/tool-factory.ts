/**
 * Tool registry. Two registration paths:
 *
 *   1. `registerTool(new TapTool(...))` — class-based, preferred.
 *      Construct the tool with its strategy dependencies, hand it to
 *      the factory. The factory binds ctx into a closure and exposes
 *      a ToolDefinition for MCP.
 *
 *   2. `register({ name, description, inputSchema, handler })` —
 *      legacy inline form, kept for tools not yet converted to classes.
 *
 * Both forms end up in the same internal store so MCP dispatch doesn't
 * know (or care) which path was used.
 */

import type { ToolDefinition } from "../types.js";
import type { AtomyxContext } from "../runtime/atomyx-context.js";
import type { Tool, ToolShape } from "./core/tool.js";

/**
 * The registry stores tools as the most permissive variant. Concrete
 * generics on each tool definition are preserved at the call site; only
 * the dispatch boundary loses them.
 */
export type AnyToolDefinition = ToolDefinition<any, unknown>;

export class ToolFactory {
  private tools = new Map<string, AnyToolDefinition>();

  constructor(private readonly ctx: AtomyxContext) {}

  /**
   * Register a class-based Tool instance. The factory binds `ctx` into
   * the handler closure and stores the resulting ToolDefinition.
   */
  registerTool(tool: Tool<any>): this {
    return this.register(tool.toDefinition(this.ctx));
  }

  /**
   * Register a legacy inline ToolDefinition. Used by tool categories
   * that haven't been converted to classes yet.
   *
   * The stored handler is wrapped with per-call timing instrumentation.
   * Every tool invocation — class-based or inline — logs one line to
   * stderr in the format:
   *
   *     [tool-timing] <name> <ms>ms <ok|err>
   *
   * stderr, not stdout, because MCP uses stdout for JSON-RPC framing.
   * Grep with: `2>&1 >/dev/null | grep tool-timing`.
   * Disable by setting `ATOMYX_TOOL_TIMING=0`.
   */
  register<TArgs, TResult>(tool: ToolDefinition<TArgs, TResult>): this {
    if (this.tools.has(tool.name)) {
      throw new Error(`duplicate tool name: ${tool.name}`);
    }
    this.tools.set(tool.name, wrapWithTiming(tool) as AnyToolDefinition);
    return this;
  }

  build(): ReadonlyArray<AnyToolDefinition> {
    return Array.from(this.tools.values());
  }

  byName(name: string): AnyToolDefinition | undefined {
    return this.tools.get(name);
  }

  size(): number {
    return this.tools.size;
  }
}

/**
 * A category registers its tools against a factory using a context.
 * Each tools/*.tools.ts exports one of these.
 */
export type ToolCategory = (factory: ToolFactory, ctx: AtomyxContext) => void;

/**
 * Wrap a tool definition's handler with start/end timing instrumentation.
 * Emits one stderr line per invocation for slow-tool triage. Respects
 * the `ATOMYX_TOOL_TIMING=0` env var to opt out (e.g. in tests where the
 * stderr noise is undesirable).
 */
function wrapWithTiming<TArgs, TResult>(
  tool: ToolDefinition<TArgs, TResult>,
): ToolDefinition<TArgs, TResult> {
  if (process.env.ATOMYX_TOOL_TIMING === "0") return tool;

  const original = tool.handler;
  return {
    ...tool,
    handler: async (args: TArgs) => {
      const start = performance.now();
      try {
        const result = await original(args);
        logTiming(tool.name, performance.now() - start, "ok");
        return result;
      } catch (err) {
        logTiming(tool.name, performance.now() - start, "err");
        throw err;
      }
    },
  };
}

function logTiming(name: string, elapsedMs: number, status: "ok" | "err"): void {
  const ms = Math.round(elapsedMs);
  // Pad name to align columns in a tailing terminal.
  const padded = name.padEnd(28);
  process.stderr.write(`[tool-timing] ${padded} ${String(ms).padStart(6)}ms ${status}\n`);
}
