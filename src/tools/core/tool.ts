import type { AdetContext } from "../../runtime/adet-context.js";
import type { JsonSchema, ToolDefinition } from "../../types.js";

/**
 * Shape bundle for a Tool: carries the concrete Args and Result types
 * so a single generic parameter on Tool<TShape> captures both.
 */
export interface ToolShape {
  args: unknown;
  result: unknown;
}

/**
 * Abstract base class for every MCP tool. Three contract fields
 * (`name`, `description`, `schema`) define the MCP boundary; one method
 * (`execute`) implements the business logic.
 *
 * Rules:
 *   - Tools are stateless across invocations. All per-call state lives
 *     in the ExecutionContext passed to `execute()`.
 *   - Tools receive their collaborators (strategies, pipelines, guards)
 *     via constructor injection. No internal `new` calls.
 *   - Tools delegate business rules to injected strategy classes — the
 *     `execute()` method is orchestration, not logic.
 *   - Tools do not access device HTTP endpoints directly. Only through
 *     the DeviceController interface on the context.
 *
 * Concrete tools:
 *   class TapTool extends Tool<{ args: TapArgs, result: TapResult }> {
 *     constructor(
 *       private readonly resolver: SelectorResolutionPipeline,
 *       private readonly imeGuard: ImeGeometricGuard,
 *       private readonly fuzzy: FuzzyResourceMatcher,
 *     ) { super(); }
 *
 *     readonly name = "tap";
 *     readonly description = "...";
 *     readonly schema = { ... };
 *
 *     async execute(args, ctx) { ... }
 *   }
 */
export abstract class Tool<TShape extends ToolShape> {
  abstract readonly name: string;
  abstract readonly description: string;
  abstract readonly schema: JsonSchema;

  abstract execute(args: TShape["args"], ctx: AdetContext): Promise<TShape["result"]>;

  /**
   * Compile this Tool instance into a MCP-compatible ToolDefinition.
   * Called by the ToolFactory at registration time — binds `ctx` into a
   * closure so the MCP dispatcher can invoke the handler with args only.
   */
  toDefinition(ctx: AdetContext): ToolDefinition<TShape["args"], TShape["result"]> {
    return {
      name: this.name,
      description: this.description,
      inputSchema: this.schema,
      handler: (args) => this.execute(args, ctx),
    };
  }
}
