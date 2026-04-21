import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { defineCommand } from "@atomyx/driver/script";
import type { BranchStep, ScriptStep } from "@atomyx/shared/script";
import { parseScript } from "../parser/yaml-parser.js";

function resolveDoBlock(
  doValue: readonly ScriptStep[] | string,
): readonly ScriptStep[] {
  if (typeof doValue === "string") {
    const filePath = resolve(doValue);
    const yaml = readFileSync(filePath, "utf-8");
    return parseScript(yaml).steps;
  }
  return doValue;
}

function resolveDotPath(obj: unknown, path: string): unknown {
  const normalized = path.startsWith("$.")
    ? path.slice(2)
    : path.startsWith("$")
      ? path.slice(1)
      : path;
  if (!normalized) return obj;
  let current: unknown = obj;
  for (const seg of normalized.split(/\./).flatMap((s) => {
    const m = s.match(/^(\w+)\[(\d+)\]$/);
    return m ? [m[1]!, m[2]!] : [s];
  })) {
    if (current == null || typeof current !== "object") return undefined;
    const idx = Number(seg);
    current = !Number.isNaN(idx) && Array.isArray(current)
      ? current[idx]
      : (current as Record<string, unknown>)[seg];
  }
  return current;
}

function deepEqual(a: unknown, b: unknown): boolean {
  if (a === b) return true;
  if (a == null || b == null || typeof a !== typeof b) return false;
  if (typeof a !== "object") return false;
  const aKeys = Object.keys(a as Record<string, unknown>);
  const bKeys = Object.keys(b as Record<string, unknown>);
  if (aKeys.length !== bKeys.length) return false;
  return aKeys.every((k) =>
    deepEqual(
      (a as Record<string, unknown>)[k],
      (b as Record<string, unknown>)[k],
    ),
  );
}

export const branchCommand = defineCommand<BranchStep>({
  command: "branch",
  async execute(args, ctx) {
    const fromKey = args.from.replace(/^\$\{(.+)\}$/, "$1");
    const captured = ctx.captures.get(fromKey);
    if (!captured) {
      return {
        ok: false,
        detail: `No captured request named "${fromKey}". Run a "capture" step first.`,
      };
    }

    for (const c of args.on) {
      let matched = true;

      if (c.match.status !== undefined && captured.status !== c.match.status) {
        matched = false;
      }

      if (c.match.body && matched) {
        for (const [path, expected] of Object.entries(c.match.body)) {
          const actual = resolveDotPath(captured.body, path);
          if (!deepEqual(actual, expected)) {
            matched = false;
            break;
          }
        }
      }

      if (matched) {
        const steps = resolveDoBlock(c.do);
        return ctx.runSteps(steps);
      }
    }

    // No case matched — run default if provided
    if (args.default) {
      const steps = resolveDoBlock(args.default);
      return ctx.runSteps(steps);
    }

    return {
      ok: false,
      detail: `branch: no case matched for "${fromKey}" (status=${captured.status})`,
    };
  },
});
