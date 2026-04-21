import { z } from "zod";

/**
 * Zod schema for the `Selector` shape exported by `@atomyx/core`.
 * Defined here (server-mcp) rather than in core because Zod is
 * an MCP/runtime-validation concern — core stays Zod-free for
 * zero-dep purity. Tools that accept selectors share this
 * schema so MCP clients see a consistent shape.
 *
 * Pattern fields accept either a literal string (exact match)
 * OR a `{ regex: string, flags?: string }` shape that the tool
 * compiles to a RegExp before passing to Orchestra. Plain string
 * is far more common; the regex form is an escape hatch for
 * advanced selectors.
 */
const PatternSchema = z.union([
  z.string(),
  z.object({
    regex: z.string(),
    flags: z.string().optional(),
  }),
]);

export const SelectorSchema = z
  .object({
    id: PatternSchema.optional(),
    text: PatternSchema.optional(),
    label: PatternSchema.optional(),
    hint: PatternSchema.optional(),
    value: PatternSchema.optional(),
    role: z.string().optional(),
    enabled: z.boolean().optional(),
    clickable: z.boolean().optional(),
    focused: z.boolean().optional(),
    nth: z.number().int().nonnegative().optional(),
  })
  .strict();

export type SelectorInput = z.infer<typeof SelectorSchema>;

/**
 * Compile a wire-shape selector into the runtime `Selector` that
 * `@atomyx/core` consumes. Converts `{regex, flags}` objects into
 * `RegExp` instances; string fields pass through unchanged.
 *
 * Importing the runtime `Selector` type is intentionally avoided
 * — we return a structurally-compatible object and let the
 * caller pass it directly to Orchestra. Keeps server-mcp free of
 * type drift if core ever extends the Selector interface.
 */
export function compileSelectorInput(input: SelectorInput): {
  id?: string | RegExp;
  text?: string | RegExp;
  label?: string | RegExp;
  hint?: string | RegExp;
  value?: string | RegExp;
  role?: string;
  enabled?: boolean;
  clickable?: boolean;
  focused?: boolean;
  nth?: number;
} {
  const out: Record<string, unknown> = {};
  if (input.id !== undefined) out.id = compilePattern(input.id);
  if (input.text !== undefined) out.text = compilePattern(input.text);
  if (input.label !== undefined) out.label = compilePattern(input.label);
  if (input.hint !== undefined) out.hint = compilePattern(input.hint);
  if (input.value !== undefined) out.value = compilePattern(input.value);
  if (input.role !== undefined) out.role = input.role;
  if (input.enabled !== undefined) out.enabled = input.enabled;
  if (input.clickable !== undefined) out.clickable = input.clickable;
  if (input.focused !== undefined) out.focused = input.focused;
  if (input.nth !== undefined) out.nth = input.nth;
  return out as ReturnType<typeof compileSelectorInput>;
}

function compilePattern(p: string | { regex: string; flags?: string }): string | RegExp {
  if (typeof p === "string") return p;
  return new RegExp(p.regex, p.flags);
}
