import { z } from "zod";
import { defineTool } from "../tool-definition.js";
// Note: report_bug deliberately does NOT use `orchestraOrFail`.
// If no device is selected and the caller asked for a
// screenshot, we skip the screenshot (with a warning) but still
// record the bug — losing a bug report because the device went
// away is worse than losing the screenshot.

const ReportBugArgs = z
  .object({
    title: z.string().min(1).describe("One-line bug title."),
    description: z
      .string()
      .min(1)
      .describe(
        "Full description: expected vs actual, repro steps, any context " +
          "the agent gathered (selectors tried, tool results, etc.).",
      ),
    captureScreenshot: z
      .boolean()
      .optional()
      .describe("Capture a screenshot as evidence. Default true."),
  })
  .strict();

/**
 * `report_bug` — record a bug finding against the currently-
 * active run. Optionally captures a screenshot at the moment
 * of the report and attaches the file path to the bug record.
 * The structured record is saved via `ctx.storage` at
 * `bugs/<id>`.
 */
export const reportBugTool = defineTool({
  name: "report_bug",
  description:
    "Record a bug finding in the currently-active run. Optionally captures " +
    "a screenshot. Use this AFTER confirming the bug by observing the device " +
    "state. Returns the bug id for reference.",
  inputSchema: ReportBugArgs,
  async execute(args, ctx) {
    const current = ctx.runStore.current();
    if (!current) {
      return {
        ok: false,
        reason: "no active run — call start_run first before reporting bugs",
      };
    }

    // Record the bug first so we have the canonical id that the
    // RunStore assigned. Screenshot path is computed from that id,
    // keeping the screenshot key a stable sibling of the bug record
    // key (`bugs/<bugId>` + `bugs/<bugId>/screenshot`). If screenshot
    // capture fails, we still persist the bug record — a bug without
    // evidence is better than a lost bug report.
    const bug = ctx.runStore.recordBug({
      title: args.title,
      description: args.description,
    });

    let screenshotPath: string | undefined;
    if (args.captureScreenshot !== false) {
      const active = ctx.session.current();
      if (!active) {
        ctx.logger.warn(
          "report_bug: no active device, skipping screenshot capture",
          { bugId: bug.id },
        );
      } else {
        try {
          const bytes = await active.orchestra.screenshot();
          const base64 = Buffer.from(bytes).toString("base64");
          const key = `bugs/${bug.id}/screenshot`;
          await ctx.storage.save(key, {
            base64,
            format: "png",
            capturedAt: Date.now(),
          });
          screenshotPath = key;
        } catch (err) {
          ctx.logger.warn("report_bug: screenshot failed", {
            error: (err as Error).message,
          });
        }
      }
    }

    // Persist the bug record separately from the run so consumers
    // can index bugs directly without opening the full run record.
    await ctx.storage.save(`bugs/${bug.id}`, {
      id: bug.id,
      runId: current.id,
      runName: current.name,
      title: bug.title,
      description: bug.description,
      screenshotPath,
      timestamp: bug.timestamp,
    });

    ctx.logger.info("bug.reported", { id: bug.id, title: bug.title });
    return {
      ok: true,
      id: bug.id,
      runId: current.id,
      screenshotPath,
    };
  },
});
