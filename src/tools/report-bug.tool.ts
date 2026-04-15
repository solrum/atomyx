import { writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import type { BugSeverity } from "../state/results.js";
import type { AtomyxContext } from "../runtime/atomyx-context.js";
import { requireController } from "../runtime/atomyx-context.js";
import type { JsonSchema } from "../types.js";
import { Tool } from "./core/tool.js";

function saveBugScreenshot(base64: string, bugId: string): string {
  const path = join(tmpdir(), `atomyx-bug-${bugId}-${Date.now()}.png`);
  writeFileSync(path, Buffer.from(base64, "base64"));
  return path;
}

export interface ReportBugArgs {
  severity: BugSeverity;
  title: string;
  description?: string;
  captureScreenshot?: boolean;
  context?: Record<string, unknown>;
}

export interface ReportBugResult {
  ok: true;
  bugId: string;
  screenshotPath?: string;
}

export class ReportBugTool extends Tool<{
  args: ReportBugArgs;
  result: ReportBugResult;
}> {
  readonly name = "report_bug";
  readonly description =
    "Record a bug found during testing. Captures a screenshot + tree snapshot for context. " +
    "Severity: low | medium | high | critical.";
  readonly schema: JsonSchema = {
    type: "object",
    required: ["severity", "title"],
    properties: {
      severity: { type: "string", enum: ["low", "medium", "high", "critical"] },
      title: { type: "string" },
      description: { type: "string" },
      captureScreenshot: { type: "boolean", default: true },
      context: { type: "object" },
    },
  };

  async execute(args: ReportBugArgs, ctx: AtomyxContext): Promise<ReportBugResult> {
    const ctl = requireController(ctx);
    let screenshotPath: string | undefined;
    let treeSnapshot: unknown;
    if (args.captureScreenshot !== false) {
      try {
        const { base64 } = await ctl.screenshot();
        screenshotPath = saveBugScreenshot(base64, `pending-${Date.now()}`);
      } catch {
        // best-effort
      }
    }
    try {
      treeSnapshot = await ctl.getUiTree();
    } catch {
      // best-effort
    }
    const bug = ctx.results.reportBug({
      severity: args.severity,
      title: args.title,
      description: args.description,
      screenshotPath,
      treeSnapshot,
      context: args.context,
    });
    return { ok: true, bugId: bug.id, screenshotPath };
  }
}
