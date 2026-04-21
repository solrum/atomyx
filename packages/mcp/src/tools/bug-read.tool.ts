import { z } from "zod";
import { defineTool } from "../tool-definition.js";

/**
 * Read-only bug query tools — `list_bugs`, `get_bug`. Symmetric
 * to `run-read.tool.ts`: read from `ctx.storage`, never touch the
 * device or `RunStore`.
 *
 * Storage layout written by `reportBugTool`:
 *
 *   bugs/<id>              ← top-level bug record (JSON)
 *   bugs/<id>/screenshot   ← screenshot payload (JSON, base64 body)
 *
 * `list_bugs` must filter nested screenshot keys out of the result
 * so agents see one entry per bug, not one-per-bug-plus-screenshot.
 * See `isTopLevelBugKey` below.
 */

interface PersistedBug {
  id: string;
  runId: string;
  runName?: string;
  title: string;
  description: string;
  screenshotPath?: string;
  timestamp: number;
}

/**
 * True iff `key` is a top-level bug record (e.g. `bugs/abc-1`) and
 * not a nested sub-record (e.g. `bugs/abc-1/screenshot`). The prefix
 * is fixed to `bugs/` and everything after the second slash means a
 * nested payload that the wire list should not surface as its own
 * bug.
 */
function isTopLevelBugKey(key: string): boolean {
  const prefix = "bugs/";
  if (!key.startsWith(prefix)) return false;
  const suffix = key.slice(prefix.length);
  return !suffix.includes("/");
}

const ListBugsArgs = z
  .object({
    runId: z
      .string()
      .optional()
      .describe("Filter bugs to a specific run id. Omit to list all bugs."),
    limit: z
      .number()
      .int()
      .positive()
      .optional()
      .describe("Maximum bugs to return per page. Default 50."),
    offset: z
      .number()
      .int()
      .nonnegative()
      .optional()
      .describe(
        "Skip this many bugs from the start of the filtered+sorted " +
          "list before applying `limit`. Use for pagination. Default 0.",
      ),
  })
  .strict();

export const listBugsTool = defineTool({
  name: "list_bugs",
  description:
    "List persisted bug reports from storage, sorted by timestamp " +
    "descending (newest first). Returns summaries only — use get_bug " +
    "for the full record. Supports runId filter and offset/limit " +
    "pagination. Response includes `totalMatching` + `nextOffset` for " +
    "easy paging.",
  inputSchema: ListBugsArgs,
  async execute(args, ctx) {
    const limit = args.limit ?? 50;
    const offset = args.offset ?? 0;
    const rawKeys = await ctx.storage.list("bugs");
    const topLevelKeys = rawKeys.filter(isTopLevelBugKey);

    const records: PersistedBug[] = [];
    for (const key of topLevelKeys) {
      const record = await ctx.storage.load<PersistedBug>(key);
      if (record) records.push(record);
    }

    const filtered = args.runId
      ? records.filter((b) => b.runId === args.runId)
      : records;

    filtered.sort((a, b) => (b.timestamp ?? 0) - (a.timestamp ?? 0));

    const summaries = filtered.slice(offset, offset + limit).map((b) => ({
      id: b.id,
      runId: b.runId,
      runName: b.runName,
      title: b.title,
      timestamp: b.timestamp,
      hasScreenshot: !!b.screenshotPath,
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
      bugs: summaries,
    };
  },
});

const GetBugArgs = z
  .object({
    id: z.string().min(1).describe("Bug id as returned by report_bug."),
  })
  .strict();

export const getBugTool = defineTool({
  name: "get_bug",
  description:
    "Fetch a single persisted bug report by id. Returns the full record " +
    "including description and screenshotPath. The screenshot bytes live " +
    "under the returned screenshotPath key and can be loaded separately by " +
    "consumers that need them. Returns ok:false when the id is not found.",
  inputSchema: GetBugArgs,
  async execute(args, ctx) {
    const record = await ctx.storage.load<PersistedBug>(`bugs/${args.id}`);
    if (!record) {
      return { ok: false, reason: `bug not found: ${args.id}` };
    }
    return { ok: true, bug: record };
  },
});

const DeleteBugArgs = z
  .object({
    id: z.string().min(1).describe("Bug id to delete."),
  })
  .strict();

export const deleteBugTool = defineTool({
  name: "delete_bug",
  description:
    "Delete a persisted bug report. Removes BOTH `bugs/<id>` and its " +
    "sibling `bugs/<id>/screenshot` key when present, so no orphan " +
    "screenshot payloads are left behind. Returns ok:false when the id " +
    "is not found (before any delete happens, so partial-delete states " +
    "are impossible).",
  inputSchema: DeleteBugArgs,
  async execute(args, ctx) {
    const bugKey = `bugs/${args.id}`;
    const existing = await ctx.storage.load<PersistedBug>(bugKey);
    if (!existing) {
      return { ok: false, reason: `bug not found: ${args.id}` };
    }
    await ctx.storage.delete(bugKey);
    // Screenshot is a stable sibling key per report-bug.tool.ts — if
    // it wasn't captured, delete is a no-op (Storage.delete tolerates
    // missing keys per its contract).
    await ctx.storage.delete(`bugs/${args.id}/screenshot`);
    ctx.logger.info("bug.deleted", { id: args.id });
    return { ok: true, id: args.id };
  },
});
