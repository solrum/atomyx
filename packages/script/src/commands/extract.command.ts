import { defineCommand } from "@atomyx/driver/script";
import type { ExtractStep } from "@atomyx/shared/script";

/**
 * Resolve a dot-path like `$.body.token` or `$.status` against
 * a captured request object. Reuses the same logic as assertApi.
 */
function resolveDotPath(obj: unknown, path: string): unknown {
  const normalized = path.startsWith("$.")
    ? path.slice(2)
    : path.startsWith("$")
      ? path.slice(1)
      : path;

  if (!normalized) return obj;

  let current: unknown = obj;
  const segments = normalized.split(/\./).flatMap((seg) => {
    const match = seg.match(/^(\w+)\[(\d+)\]$/);
    if (match) return [match[1]!, match[2]!];
    return [seg];
  });

  for (const segment of segments) {
    if (current === null || current === undefined) return undefined;
    if (typeof current !== "object") return undefined;
    const idx = Number(segment);
    if (!Number.isNaN(idx) && Array.isArray(current)) {
      current = current[idx];
    } else {
      current = (current as Record<string, unknown>)[segment];
    }
  }

  return current;
}

export const extractCommand = defineCommand<ExtractStep>({
  command: "extract",
  async execute(args, ctx) {
    const fromKey = args.from.replace(/^\$\{(.+)\}$/, "$1");
    const captured = ctx.captures.get(fromKey);
    if (!captured) {
      return {
        ok: false,
        detail:
          `No captured request named "${fromKey}". ` +
          `Run a "capture" step with "as: ${fromKey}" first.`,
      };
    }

    const extracted: string[] = [];
    for (const [name, path] of Object.entries(args.values)) {
      const value = resolveDotPath(captured, path);
      if (value === undefined) {
        return {
          ok: false,
          detail: `Path "${path}" resolved to undefined in captured "${fromKey}"`,
        };
      }
      ctx.variables.set(name, String(value));
      extracted.push(`${name}=${JSON.stringify(value)}`);
    }

    return {
      ok: true,
      detail: `Extracted: ${extracted.join(", ")}`,
    };
  },
});
