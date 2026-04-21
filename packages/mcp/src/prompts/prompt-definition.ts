/**
 * MCP prompt definition contract.
 *
 * MCP's `prompts/` capability lets a server ship named prompt
 * templates that the client can surface as slash commands,
 * auto-load into context, or preview to the user. Atomyx uses
 * this to distribute **testing methodology** — the "how to run
 * a test session" guidance that is neither a tool description
 * (which explains a single tool) nor a user's project-specific
 * system prompt (which belongs in their client config).
 *
 * Methodology prompts ship with the server so users get
 * framework-grade testing workflows out of the box:
 *
 *   - Install `@atomyx/cli`
 *   - Start the MCP server
 *   - Client calls `prompts/list` and discovers
 *     `atomyx/exploratory`, `atomyx/regression`, etc.
 *   - User invokes `/atomyx/exploratory` → client fetches the
 *     template via `prompts/get` and injects it into the
 *     conversation
 *   - Agent now has a complete playbook for how to use the
 *     tool surface
 *
 * Users never have to hand-copy methodology into each client's
 * config file.
 */

/**
 * Shape of a prompt argument — matches the MCP protocol's
 * `PromptArgument` declaration. Rendered prompts substitute
 * `{{arg}}` placeholders in the template with the provided
 * values.
 */
export interface PromptArgument {
  readonly name: string;
  readonly description: string;
  readonly required?: boolean;
}

/**
 * A single prompt message — always in `{role, content}` shape
 * matching MCP's `PromptMessage`. For methodology prompts we
 * typically emit a single `user` message with the full
 * template; the array shape exists so future prompts can ship
 * multi-turn priming sequences without a schema change.
 */
export interface PromptMessage {
  readonly role: "user" | "assistant";
  readonly content: {
    readonly type: "text";
    readonly text: string;
  };
}

/**
 * Definition of a single prompt. `render()` receives the
 * argument bag the client passed to `prompts/get` and returns
 * the fully-materialized messages the client will inject.
 *
 * Keeping `render()` as a function (instead of a static
 * template string) lets prompt authors compose arguments into
 * the output — e.g. inline an `appId` into headings, or skip
 * optional sections when an argument is absent.
 */
export interface PromptDefinition {
  readonly name: string;
  readonly description: string;
  readonly arguments?: readonly PromptArgument[];
  render(args: Record<string, string>): readonly PromptMessage[];
}

/**
 * Helper that returns its argument unchanged but gives
 * inference a nudge for authors writing prompt files. Mirrors
 * the `defineTool` pattern in `tool-definition.ts`.
 */
export function definePrompt(def: PromptDefinition): PromptDefinition {
  return def;
}

/**
 * Minimal `{{name}}` → value substitution for prompt templates.
 * Keeps our prompts template-engine-free. Missing keys are left
 * as-is (useful for literal mentions of `{{curly}}` text).
 */
export function interpolate(
  template: string,
  args: Record<string, string>,
): string {
  return template.replace(/\{\{(\w+)\}\}/g, (match, key) => {
    return args[key] ?? match;
  });
}
