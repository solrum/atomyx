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
import type { AdetContext } from "../runtime/adet-context.js";
import type { Tool, ToolShape } from "./core/tool.js";

/**
 * The registry stores tools as the most permissive variant. Concrete
 * generics on each tool definition are preserved at the call site; only
 * the dispatch boundary loses them.
 */
export type AnyToolDefinition = ToolDefinition<any, unknown>;

export class ToolFactory {
  private tools = new Map<string, AnyToolDefinition>();

  constructor(private readonly ctx: AdetContext) {}

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
   */
  register<TArgs, TResult>(tool: ToolDefinition<TArgs, TResult>): this {
    if (this.tools.has(tool.name)) {
      throw new Error(`duplicate tool name: ${tool.name}`);
    }
    this.tools.set(tool.name, tool as AnyToolDefinition);
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
export type ToolCategory = (factory: ToolFactory, ctx: AdetContext) => void;
