import { z } from "zod";
import { defineTool } from "../tool-definition.js";

/**
 * Run lifecycle tools — `start_run`, `finish_run`. Manage the
 * in-memory `RunStore` on the tool context. Finishing a run
 * triggers persistence via `ctx.storage` so the run record
 * outlives the process for reporting / replay.
 */

const StartRunArgs = z
  .object({
    name: z.string().min(1).describe("Human description of the run."),
    source: z
      .string()
      .optional()
      .describe("Mode label: exploratory, regression, bug-repro, smoke, etc."),
  })
  .strict();

export const startRunTool = defineTool({
  name: "start_run",
  description:
    "Start a new test run. Call this BEFORE driving the device so bug reports " +
    "and case studies can be attached to a named run. Any previously-active run " +
    "is implicitly force-closed as 'error' and its record is persisted to " +
    "runs/<id> so the lost run's trail isn't dropped — the returned " +
    "`erroredPredecessor` field carries the old run id when this happens.",
  inputSchema: StartRunArgs,
  async execute(args, ctx) {
    const { run, erroredPredecessor } = ctx.runStore.start({
      name: args.name,
      source: args.source,
    });

    // Persist the force-errored predecessor, if any. Without this,
    // agents that forget `finish_run` silently lose their previous
    // run's history — see the "lifecycle edge cases" discussion in
    // run-store.ts for the rationale.
    let erroredPredecessorId: string | undefined;
    if (erroredPredecessor) {
      erroredPredecessorId = erroredPredecessor.id;
      await ctx.storage.save(`runs/${erroredPredecessor.id}`, {
        id: erroredPredecessor.id,
        name: erroredPredecessor.name,
        source: erroredPredecessor.source,
        status: erroredPredecessor.status,
        startedAt: erroredPredecessor.startedAt,
        finishedAt: erroredPredecessor.finishedAt,
        durationMs:
          (erroredPredecessor.finishedAt ?? Date.now()) - erroredPredecessor.startedAt,
        actionCount: erroredPredecessor.actionCount,
        findings: erroredPredecessor.findings,
        summary: "force-closed by subsequent start_run — agent forgot finish_run",
      });
      ctx.logger.warn("run.force_closed_predecessor", {
        id: erroredPredecessor.id,
        newRunId: run.id,
      });
    }

    ctx.logger.info("run.started", { id: run.id, name: run.name });
    return {
      ok: true,
      id: run.id,
      name: run.name,
      source: run.source,
      startedAt: run.startedAt,
      erroredPredecessor: erroredPredecessorId,
    };
  },
});

const FinishRunArgs = z
  .object({
    status: z
      .union([z.literal("passed"), z.literal("failed"), z.literal("error")])
      .optional()
      .describe("Final verdict. Defaults to 'passed' when omitted."),
    summary: z.string().optional(),
  })
  .strict();

export const finishRunTool = defineTool({
  name: "finish_run",
  description:
    "Finish the currently-active test run with a verdict and optional summary. " +
    "Persists the run record to storage (key: runs/<id>) so it can be read back " +
    "or exported to a reporter. Returns { ok, id, status, actionCount, findingsCount, durationMs }.",
  inputSchema: FinishRunArgs,
  async execute(args, ctx) {
    const run = ctx.runStore.finish(args.status ?? "passed");
    if (!run) {
      return { ok: false, reason: "no active run to finish" };
    }
    const record = {
      id: run.id,
      name: run.name,
      source: run.source,
      status: run.status,
      startedAt: run.startedAt,
      finishedAt: run.finishedAt,
      durationMs: (run.finishedAt ?? Date.now()) - run.startedAt,
      actionCount: run.actionCount,
      findings: run.findings,
      summary: args.summary,
    };
    await ctx.storage.save(`runs/${run.id}`, record);
    ctx.logger.info("run.finished", {
      id: run.id,
      status: run.status,
      duration: record.durationMs,
    });
    return {
      ok: true,
      id: run.id,
      status: run.status,
      actionCount: run.actionCount,
      findingsCount: run.findings.length,
      durationMs: record.durationMs,
    };
  },
});
