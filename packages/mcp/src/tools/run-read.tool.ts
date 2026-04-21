import { z } from "zod";
import { defineTool } from "../tool-definition.js";

/**
 * Read-only run query tools — `list_runs`, `get_run`. These read
 * from `ctx.storage` (the port the run-lifecycle tools persist
 * through) and never touch the device or the in-memory `RunStore`.
 *
 * Why separate from `run-lifecycle.tool.ts`: the mutating lifecycle
 * tools and the query tools have different dependency shapes
 * (lifecycle mutates RunStore, query only reads Storage). Splitting
 * makes intent grep-friendly and keeps each file narrow.
 */

/**
 * Persisted run record shape. Mirrors what `finishRunTool` and the
 * force-closed persistence path in `startRunTool` write to storage,
 * plus the optional `summary` field. Not exported — tools return
 * `unknown`-shaped JSON to agents and the agent consumes the wire
 * shape directly.
 */
interface PersistedRun {
  id: string;
  name: string;
  source: string;
  status: "running" | "passed" | "failed" | "error";
  startedAt: number;
  finishedAt?: number;
  durationMs?: number;
  actionCount: number;
  findings: Array<{ id: string; title: string }>;
  summary?: string;
}

const RunStatusEnum = z.union([
  z.literal("passed"),
  z.literal("failed"),
  z.literal("error"),
  z.literal("running"),
]);

const ListRunsArgs = z
  .object({
    status: RunStatusEnum.optional().describe(
      "Filter by verdict status. Omit to return all statuses.",
    ),
    limit: z
      .number()
      .int()
      .positive()
      .optional()
      .describe("Maximum runs to return per page. Default 20."),
    offset: z
      .number()
      .int()
      .nonnegative()
      .optional()
      .describe(
        "Skip this many runs from the start of the filtered+sorted " +
          "list before applying `limit`. Use for pagination. Default 0. " +
          "Note: offsets are relative to the current snapshot — if a new " +
          "run finishes between pages, the offset shifts. Agents that " +
          "care about consistency across pages should snapshot the full " +
          "list in one call with a large `limit`.",
      ),
  })
  .strict();

export const listRunsTool = defineTool({
  name: "list_runs",
  description:
    "List persisted runs from storage, sorted by startedAt descending " +
    "(most recent first). Returns summaries only — use get_run to fetch the " +
    "full record including findings. Supports status filter and " +
    "offset/limit pagination. The response includes `totalMatching` so " +
    "callers can decide whether to request another page.",
  inputSchema: ListRunsArgs,
  async execute(args, ctx) {
    const limit = args.limit ?? 20;
    const offset = args.offset ?? 0;
    const keys = await ctx.storage.list("runs");

    const records: PersistedRun[] = [];
    for (const key of keys) {
      const record = await ctx.storage.load<PersistedRun>(key);
      if (record) records.push(record);
    }

    const filtered = args.status
      ? records.filter((r) => r.status === args.status)
      : records;

    // Sort desc by startedAt. Missing startedAt (shouldn't happen for
    // well-formed records) sinks to the bottom.
    filtered.sort((a, b) => (b.startedAt ?? 0) - (a.startedAt ?? 0));

    const summaries = filtered
      .slice(offset, offset + limit)
      .map((r) => ({
        id: r.id,
        name: r.name,
        source: r.source,
        status: r.status,
        startedAt: r.startedAt,
        finishedAt: r.finishedAt,
        durationMs: r.durationMs,
        actionCount: r.actionCount,
        findingsCount: r.findings?.length ?? 0,
      }));

    return {
      ok: true,
      count: summaries.length,
      totalMatching: filtered.length,
      offset,
      nextOffset:
        offset + summaries.length < filtered.length
          ? offset + summaries.length
          : null,
      runs: summaries,
    };
  },
});

const GetRunArgs = z
  .object({
    id: z.string().min(1).describe("Run id as returned by start_run."),
  })
  .strict();

export const getRunTool = defineTool({
  name: "get_run",
  description:
    "Fetch a single persisted run by id. Returns the full record " +
    "including findings and summary. Use list_runs first to discover " +
    "available ids. Returns ok:false when the id is not found.",
  inputSchema: GetRunArgs,
  async execute(args, ctx) {
    const record = await ctx.storage.load<PersistedRun>(`runs/${args.id}`);
    if (!record) {
      return { ok: false, reason: `run not found: ${args.id}` };
    }
    return { ok: true, run: record };
  },
});

const DeleteRunArgs = z
  .object({
    id: z.string().min(1).describe("Run id to delete."),
  })
  .strict();

export const deleteRunTool = defineTool({
  name: "delete_run",
  description:
    "Delete a persisted run record. Removes `runs/<id>` from storage. " +
    "Associated bug records (`bugs/<runId>-bug-<n>`) are NOT cascade-" +
    "deleted — they remain accessible via get_bug with their runId field " +
    "pointing at the now-missing run. Use list_bugs with the runId filter " +
    "then delete_bug to clean those up individually if desired. Returns " +
    "ok:false when the id is not found.",
  inputSchema: DeleteRunArgs,
  async execute(args, ctx) {
    const key = `runs/${args.id}`;
    const existing = await ctx.storage.load<PersistedRun>(key);
    if (!existing) {
      return { ok: false, reason: `run not found: ${args.id}` };
    }
    await ctx.storage.delete(key);
    ctx.logger.info("run.deleted", { id: args.id });
    return { ok: true, id: args.id };
  },
});

const UpdateRunSummaryArgs = z
  .object({
    id: z.string().min(1).describe("Run id to update."),
    summary: z
      .string()
      .describe("New summary text. Pass an empty string to clear."),
  })
  .strict();

export const updateRunSummaryTool = defineTool({
  name: "update_run_summary",
  description:
    "Set or replace the summary field on a persisted run. Useful when " +
    "the summary wasn't known at finish_run time, or to append context " +
    "discovered by a later investigation. Only the summary field is " +
    "mutated — all other fields (status, findings, durationMs, …) are " +
    "preserved. Returns ok:false when the id is not found.",
  inputSchema: UpdateRunSummaryArgs,
  async execute(args, ctx) {
    const key = `runs/${args.id}`;
    const existing = await ctx.storage.load<PersistedRun>(key);
    if (!existing) {
      return { ok: false, reason: `run not found: ${args.id}` };
    }
    const updated: PersistedRun = { ...existing, summary: args.summary };
    await ctx.storage.save(key, updated);
    ctx.logger.info("run.summary_updated", {
      id: args.id,
      length: args.summary.length,
    });
    return { ok: true, id: args.id, summary: args.summary };
  },
});
