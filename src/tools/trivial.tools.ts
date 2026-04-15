import type { AdetContext } from "../runtime/adet-context.js";
import { requireController } from "../runtime/adet-context.js";
import type { JsonSchema } from "../types.js";
import { Tool } from "./core/tool.js";

/**
 * Trivial tools — each one is pure delegation to a DeviceController
 * method (or a tiny wrapper) with no orchestration, no strategy. Rather
 * than create one file per trivial tool, they live here as a batch.
 *
 * If any of these grows orchestration logic, promote it to its own
 * `<name>.tool.ts` file and inject the necessary strategies.
 */

// ── press_key ─────────────────────────────────────────────────────────

export class PressKeyTool extends Tool<{
  args: { key: "back" | "home" | "enter" };
  result: { ok: true };
}> {
  readonly name = "press_key";
  readonly description = "Press a system key.";
  readonly schema: JsonSchema = {
    type: "object",
    required: ["key"],
    properties: { key: { type: "string", enum: ["back", "home", "enter"] } },
  };

  async execute(args: { key: "back" | "home" | "enter" }, ctx: AdetContext) {
    await requireController(ctx).pressKey(args.key);
    return { ok: true as const };
  }
}

// ── swipe ─────────────────────────────────────────────────────────────

export class SwipeTool extends Tool<{
  args: { fromX: number; fromY: number; toX: number; toY: number; durationMs?: number };
  result: { ok: boolean; reason?: string };
}> {
  readonly name = "swipe";
  readonly description =
    "Swipe from (fromX,fromY) to (toX,toY). durationMs default 300. Rejects near-zero movement " +
    "(dx<16 && dy<16) to prevent no-op gestures used as counter-reset tricks.";
  readonly schema: JsonSchema = {
    type: "object",
    required: ["fromX", "fromY", "toX", "toY"],
    properties: {
      fromX: { type: "number" },
      fromY: { type: "number" },
      toX: { type: "number" },
      toY: { type: "number" },
      durationMs: { type: "number", default: 300 },
    },
  };

  async execute(
    args: { fromX: number; fromY: number; toX: number; toY: number; durationMs?: number },
    ctx: AdetContext,
  ) {
    const ctl = requireController(ctx);
    const dx = Math.abs(args.toX - args.fromX);
    const dy = Math.abs(args.toY - args.fromY);
    if (dx < 16 && dy < 16) {
      return {
        ok: false,
        reason:
          "BLOCKED: swipe with near-zero movement is a no-op gesture. If you're trying to " +
          "reset a counter or tap, use the correct tool directly (input_text, " +
          "tap_and_wait_transition).",
      };
    }
    await ctl.swipe(args.fromX, args.fromY, args.toX, args.toY, args.durationMs);
    return { ok: true };
  }
}

// ── list_apps ─────────────────────────────────────────────────────────

export class ListAppsTool extends Tool<{
  args: Record<string, never>;
  result: { appId: string; label?: string }[];
}> {
  readonly name = "list_apps";
  readonly description = "List installed apps on the selected device.";
  readonly schema: JsonSchema = { type: "object", properties: {} };

  async execute(_args: Record<string, never>, ctx: AdetContext) {
    return requireController(ctx).listApps();
  }
}

// ── list_devices ──────────────────────────────────────────────────────

// list_devices and select_device need access to listAllDevices / connectDevice
// from device-router — they are NOT DeviceController methods. They stay in
// devices.tools.ts (inline) because they run BEFORE a controller is selected.

// ── start_run ─────────────────────────────────────────────────────────

export class StartRunTool extends Tool<{
  args: { name: string; source?: "scripted" | "exploratory" | "interactive" };
  result: { ok: true; runId: string };
}> {
  readonly name = "start_run";
  readonly description =
    "Start a named test run. Resets history + bug counter. Returns the run id. " +
    "Subsequent report_bug calls attach to this run.";
  readonly schema: JsonSchema = {
    type: "object",
    required: ["name"],
    properties: {
      name: { type: "string" },
      source: {
        type: "string",
        enum: ["scripted", "exploratory", "interactive"],
        default: "interactive",
      },
    },
  };

  async execute(
    args: { name: string; source?: "scripted" | "exploratory" | "interactive" },
    ctx: AdetContext,
  ) {
    const ctl = requireController(ctx);
    ctx.history.start();
    const run = ctx.results.startRun({
      name: args.name,
      source: args.source ?? "interactive",
      deviceId: ctl.deviceId,
      platform: ctl.platform,
    });
    return { ok: true as const, runId: run.id };
  }
}

// ── finish_run ────────────────────────────────────────────────────────

export class FinishRunTool extends Tool<{
  args: { status?: "passed" | "failed" | "error" };
  result: {
    ok: true;
    run: { id: string; name: string; status: string; bugs: number; findings: number; durationMs: number } | null;
    savedTo: string | null;
  };
}> {
  readonly name = "finish_run";
  readonly description =
    "Finalize the current test run, persist results, return summary.";
  readonly schema: JsonSchema = {
    type: "object",
    properties: {
      status: {
        type: "string",
        enum: ["passed", "failed", "error"],
        default: "passed",
      },
    },
  };

  async execute(args: { status?: "passed" | "failed" | "error" }, ctx: AdetContext) {
    const run = ctx.results.finishRun(args.status ?? "passed");
    const path = ctx.results.persistLocal();
    return {
      ok: true as const,
      run: run
        ? {
            id: run.id,
            name: run.name,
            status: run.status,
            bugs: run.bugs.length,
            findings: run.findings.length,
            durationMs: (run.finishedAt ?? Date.now()) - run.startedAt,
          }
        : null,
      savedTo: path,
    };
  }
}
