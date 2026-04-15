export type JsonSchema = {
  type?: string;
  properties?: Record<string, JsonSchema>;
  required?: string[];
  enum?: unknown[];
  default?: unknown;
  description?: string;
  items?: JsonSchema;
};

/**
 * Generic tool definition. Args are validated upstream by MCP against
 * `inputSchema`, so handlers can declare their concrete shape without
 * losing type safety. The factory's `register()` accepts any
 * `ToolDefinition<unknown>`-compatible value (variance is contravariant
 * in the argument position).
 */
export interface ToolDefinition<TArgs = Record<string, unknown>, TResult = unknown> {
  name: string;
  description: string;
  inputSchema: JsonSchema;
  handler: (args: TArgs) => Promise<TResult>;
}
