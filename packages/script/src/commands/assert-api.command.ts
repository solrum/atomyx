import { defineCommand } from "@atomyx/driver/script";
import type { AssertApiStep } from "@atomyx/shared/script";

export const assertApiCommand = defineCommand<AssertApiStep>({
  command: "assertApi",
  async execute(args, ctx) {
    // Strip ${...} wrapper if user wrote from: ${varName} instead of from: varName
    const fromKey = args.from.replace(/^\$\{(.+)\}$/, "$1");
    const captured = ctx.captures.get(fromKey);
    if (!captured) {
      return {
        ok: false,
        detail:
          `No captured request named "${args.from}". ` +
          `Did you run a "capture" step with "as: ${args.from}" before this assertion?`,
      };
    }

    const failures: string[] = [];

    // Status assertion
    if (args.status !== undefined && captured.status !== args.status) {
      failures.push(
        `status: expected ${args.status}, got ${captured.status}`,
      );
    }

    // Body assertions via dot-path with operator support
    if (args.body) {
      for (const [path, expected] of Object.entries(args.body)) {
        const actual = resolveDotPath(captured.body, path);
        const result = assertValue(actual, expected);
        if (!result.ok) {
          failures.push(`body ${path}: ${result.reason}`);
        }
      }
    }

    if (failures.length > 0) {
      return {
        ok: false,
        detail: `API assertion failed on "${args.from}":\n  - ${failures.join("\n  - ")}`,
      };
    }

    return {
      ok: true,
      detail: `API assertion passed for "${args.from}" (${captured.method} ${captured.url})`,
    };
  },
});

/**
 * Resolve a dot-path like `$.status` or `$.data.items[0].name`
 * against a JSON body. Minimal implementation — supports:
 *   - `$.key.nested`
 *   - `$.key[0]` (array index)
 *   - `$.key[0].nested`
 *
 * No full JSONPath spec — just enough for practical API
 * assertions. Extend when real use cases demand more.
 */
function resolveDotPath(body: unknown, path: string): unknown {
  // Strip leading $. prefix
  const normalized = path.startsWith("$.")
    ? path.slice(2)
    : path.startsWith("$")
      ? path.slice(1)
      : path;

  if (!normalized) return body;

  let current: unknown = body;
  // Split on . but handle [N] array indices
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

/**
 * Assert a resolved value against an expected value.
 *
 * Operators are prefixed with `$` to avoid ambiguity with
 * literal string values:
 *
 *   - `$not_empty` — value exists and is not empty/null/undefined/[]
 *   - `$exists` — value is not undefined
 *   - `$not_exists` — value is undefined
 *   - `$contains:text` — string value contains "text"
 *   - `$gt:N` / `$gte:N` — greater than / greater than or equal
 *   - `$lt:N` / `$lte:N` — less than / less than or equal
 *   - `$between:min,max` — value is between min and max (inclusive)
 *   - Any other value — exact deep equality match
 *
 * Without `$` prefix, values are compared literally:
 *   `"not_empty"` matches the actual string "not_empty"
 *   `$not_empty` checks that the value is not empty
 */
function assertValue(
  actual: unknown,
  expected: unknown,
): { ok: boolean; reason: string } {
  // Operators must start with $ — no prefix means exact match
  if (typeof expected === "string" && expected.startsWith("$")) {
    const op = expected.slice(1); // strip $

    if (op === "not_empty") {
      if (actual === undefined || actual === null || actual === "" ||
          (Array.isArray(actual) && actual.length === 0)) {
        return { ok: false, reason: `expected $not_empty, got ${JSON.stringify(actual)}` };
      }
      return { ok: true, reason: "" };
    }

    if (op === "exists") {
      if (actual === undefined) {
        return { ok: false, reason: "expected $exists, got undefined" };
      }
      return { ok: true, reason: "" };
    }

    if (op === "not_exists") {
      if (actual !== undefined) {
        return { ok: false, reason: `expected $not_exists, got ${JSON.stringify(actual)}` };
      }
      return { ok: true, reason: "" };
    }

    const containsMatch = op.match(/^contains:(.+)$/);
    if (containsMatch) {
      const needle = containsMatch[1]!;
      if (typeof actual !== "string" || !actual.includes(needle)) {
        return { ok: false, reason: `expected $contains:"${needle}", got ${JSON.stringify(actual)}` };
      }
      return { ok: true, reason: "" };
    }

    const betweenMatch = op.match(/^between:([^,]+),(.+)$/);
    if (betweenMatch) {
      const min = Number(betweenMatch[1]);
      const max = Number(betweenMatch[2]);
      const num = Number(actual);
      if (Number.isNaN(num) || Number.isNaN(min) || Number.isNaN(max)) {
        return { ok: false, reason: `cannot compare: actual=${JSON.stringify(actual)}, range=${betweenMatch[1]},${betweenMatch[2]}` };
      }
      if (num < min || num > max) {
        return { ok: false, reason: `expected $between:${min},${max}, got ${num}` };
      }
      return { ok: true, reason: "" };
    }

    const compMatch = op.match(/^(gt|gte|lt|lte):(.+)$/);
    if (compMatch) {
      const cmp = compMatch[1]!;
      const threshold = Number(compMatch[2]);
      const num = Number(actual);
      if (Number.isNaN(num) || Number.isNaN(threshold)) {
        return { ok: false, reason: `cannot compare: actual=${JSON.stringify(actual)}, threshold=${compMatch[2]}` };
      }
      const passed =
        cmp === "gt" ? num > threshold :
        cmp === "gte" ? num >= threshold :
        cmp === "lt" ? num < threshold :
        num <= threshold;
      if (!passed) {
        return { ok: false, reason: `expected $${cmp}:${threshold}, got ${num}` };
      }
      return { ok: true, reason: "" };
    }

    // Unknown operator — treat as exact match ($ might be literal)
  }

  // Default: exact deep equality
  if (!deepEqual(actual, expected)) {
    return { ok: false, reason: `expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}` };
  }
  return { ok: true, reason: "" };
}

function deepEqual(a: unknown, b: unknown): boolean {
  if (a === b) return true;
  if (a === null || b === null) return false;
  if (typeof a !== typeof b) return false;
  if (typeof a !== "object") return false;
  const aKeys = Object.keys(a as Record<string, unknown>);
  const bKeys = Object.keys(b as Record<string, unknown>);
  if (aKeys.length !== bKeys.length) return false;
  return aKeys.every((key) =>
    deepEqual(
      (a as Record<string, unknown>)[key],
      (b as Record<string, unknown>)[key],
    ),
  );
}
