import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { defineCommand } from "@atomyx/driver/script";
import type { HandleStep, ScriptSelector, ScriptStep } from "@atomyx/shared/script";
import { compileScriptSelector } from "../parser/selector-compiler.js";
import { parseScript } from "../parser/yaml-parser.js";

function toSelector(value: string | ScriptSelector): ScriptSelector {
  // Bare string matches both text + label (Flutter uses label)
  return typeof value === "string" ? { text: value, label: value } : value;
}

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

/**
 * Default poll budget for `handle` when the YAML doesn't specify a
 * timeout. Post-action transitions (tap a button, wait for the
 * next screen) usually settle within 3 s on both iOS and Android;
 * anything slower is a legitimate wait the script should make
 * explicit via `timeout:`.
 */
const DEFAULT_HANDLE_TIMEOUT_MS = 5_000;
const POLL_INTERVAL_MS = 100;

export const handleCommand = defineCommand<HandleStep>({
  command: "handle",
  async execute(args, ctx) {
    const timeoutMs = args.timeout ?? DEFAULT_HANDLE_TIMEOUT_MS;
    const deadline = ctx.clock.now() + timeoutMs;

    while (true) {
      for (const branch of args.branches) {
        let matched = true;

        if (branch.when.visible) {
          const selector = compileScriptSelector(toSelector(branch.when.visible));
          const found = await ctx.orchestra.findOne(selector);
          if (!found) matched = false;
        }

        if (branch.when.notVisible && matched) {
          const selector = compileScriptSelector(toSelector(branch.when.notVisible));
          const found = await ctx.orchestra.findOne(selector);
          if (found) matched = false;
        }

        if (matched) {
          const steps = resolveDoBlock(branch.do);
          return ctx.runSteps(steps);
        }
      }

      if (ctx.clock.now() >= deadline) break;
      await ctx.clock.sleep(POLL_INTERVAL_MS);
    }

    if (args.otherwise === "skip") {
      return { ok: true, detail: "handle: no branch matched, skipped" };
    }
    return {
      ok: false,
      detail: `handle: no branch matched within ${timeoutMs}ms (otherwise: fail)`,
    };
  },
});
