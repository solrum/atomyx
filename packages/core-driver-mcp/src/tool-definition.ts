import type { z } from "zod";
import type { Orchestra, Logger } from "@atomyx/core-driver";

/**
 * Common context every MCP tool receives at execution time.
 * Built once per `createMcpServer({orchestra,logger})` call and
 * passed into every tool's handler. Keeping it small forces
 * tools to depend only on what they actually need — no magic
 * service locator.
 *
 * Why Orchestra is the only mutator dependency: every action
 * the framework can perform on a device goes through Orchestra.
 * Tools that bypass it would re-introduce the god-class
 * coupling the framework refactor exists to eliminate.
 */
export interface ToolContext {
  readonly orchestra: Orchestra;
  readonly logger: Logger;
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
