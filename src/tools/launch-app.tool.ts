import type { AtomyxContext } from "../runtime/atomyx-context.js";
import { requireController } from "../runtime/atomyx-context.js";
import type { JsonSchema } from "../types.js";
import { StructuralInputFinder } from "./core/index.js";
import { Tool } from "./core/tool.js";
import { preflight, type PreflightResult } from "./preflight.js";
import { filterStable, renderCompactLine, sortByStability } from "./tree-render.js";

export interface LaunchAppArgs {
  appId: string;
  forceStop?: boolean;
}

export interface LaunchedApp {
  label?: string;
  stableId?: string;
  center: { x: number; y: number };
  currentValue?: string;
}

export type LaunchAppResult =
  | {
      ok: true;
      launchedAppId: string;
      initialTree: string;
      inputs: LaunchedApp[];
      elementCount: number;
      totalAvailable: number;
      instruction: string;
    }
  | { ok: false; launchedAppId: string; preflight: PreflightResult };

/**
 * Launch an app and pre-populate agent-usable addressable state for the
 * first rendered screen. Returns an `inputs[]` array (every text field
 * with its semantic label + center coords) plus `initialTree` (compact,
 * stable-only) so the agent doesn't need a separate `get_ui_tree` call.
 *
 * Depends on: StructuralInputFinder to collect the inputs[] with their
 * labels derived from preceding-sibling / parent anchors.
 */
export class LaunchAppTool extends Tool<{
  args: LaunchAppArgs;
  result: LaunchAppResult;
}> {
  readonly name = "launch_app";
  readonly description =
    "Launch an app by id (Android package name / iOS bundle id). Pass `forceStop: true` " +
    "(default) to reset state first. Returns `inputs` array with center coords for every " +
    "text field on the first rendered screen.";
  readonly schema: JsonSchema = {
    type: "object",
    required: ["appId"],
    properties: {
      appId: { type: "string" },
      forceStop: { type: "boolean", default: true },
    },
  };

  constructor(private readonly inputFinder: StructuralInputFinder) {
    super();
  }

  async execute(args: LaunchAppArgs, ctx: AtomyxContext): Promise<LaunchAppResult> {
    const ctl = requireController(ctx);
    if (args.forceStop !== false) {
      await ctl.forceStopApp(args.appId);
      await new Promise((r) => setTimeout(r, 400));
    }
    await ctl.launchApp(args.appId);
    await new Promise((r) => setTimeout(r, 500));

    const check = await preflight(ctl);
    if (!check.ok) {
      return { ok: false, launchedAppId: args.appId, preflight: check };
    }

    // Compact stable-only tree for the agent's initial read.
    const summary = await ctl.getUiSummary();
    const sorted = sortByStability(filterStable(summary));
    const limited = sorted.slice(0, 40);
    const initialTree = limited.map(renderCompactLine).join("\n");

    // Semantic input enumeration via the 4-strategy chain — labels come
    // from anchors (preceding-sibling / parent), not positional index.
    const inputs = (await this.inputFinder.collectAll(ctl)).map((inp) => ({
      label: inp.label,
      stableId: inp.stableId?.value,
      center: inp.center,
      currentValue: inp.currentValue,
    }));

    return {
      ok: true,
      launchedAppId: args.appId,
      initialTree,
      inputs,
      elementCount: limited.length,
      totalAvailable: sorted.length,
      instruction:
        "`inputs[]` = every text field with label + center coords. Call " +
        "`input_text({x, y, text})` using inputs[N].center. Match by LABEL, order varies " +
        "across screens. For buttons, read `initialTree`. Avoid extra get_ui_tree calls.",
    };
  }
}
