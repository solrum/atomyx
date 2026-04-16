import { z } from "zod";
import { defineTool } from "../tool-definition.js";

/**
 * `add_case_study` — append a learned lesson to the playbook.
 * `get_case_studies` — list + read case studies.
 *
 * Case studies are markdown records persisted via `ctx.storage`
 * under `case-studies/YYYY-MM/<title>`. Agents call
 * `add_case_study` after recovering from a non-obvious error so
 * future sessions (or future agents) can read the hard-won
 * knowledge back via `get_case_studies` at session start.
 */

const AddCaseStudyArgs = z
  .object({
    title: z.string().min(1),
    trigger: z.string().min(1).describe("What situation caused the pain."),
    solution: z.string().min(1).describe("What worked to recover."),
    example: z.string().optional().describe("Optional example snippet."),
  })
  .strict();

export const addCaseStudyTool = defineTool({
  name: "add_case_study",
  description:
    "Append a learned lesson to the case-studies playbook. Use AFTER recovering " +
    "from a non-obvious error (obscurement dismissal, scroll-search edge case, " +
    "flaky transition) so future sessions can read it. Records are markdown under " +
    "case-studies/YYYY-MM/<slug>.",
  inputSchema: AddCaseStudyArgs,
  async execute(args, ctx) {
    const d = new Date();
    const ym = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}`;
    const slug = args.title
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "")
      .slice(0, 60);
    const key = `case-studies/${ym}/${slug}`;
    const body =
      `# ${args.title}\n\n` +
      `**Trigger:** ${args.trigger}\n\n` +
      `**Solution:** ${args.solution}\n\n` +
      (args.example ? `**Example:**\n\n\`\`\`\n${args.example}\n\`\`\`\n\n` : "") +
      `_Added: ${d.toISOString()}_\n`;

    await ctx.storage.save(key, body);
    ctx.logger.info("case-study.added", { key });
    return { ok: true, key };
  },
});

const GetCaseStudiesArgs = z
  .object({
    month: z
      .string()
      .regex(/^\d{4}-\d{2}$/)
      .optional()
      .describe("Filter by YYYY-MM. If omitted, returns across all months."),
    limit: z
      .number()
      .int()
      .positive()
      .optional()
      .describe("Maximum studies to return per page. Default 20."),
    offset: z
      .number()
      .int()
      .nonnegative()
      .optional()
      .describe(
        "Skip this many studies from the sorted list before applying " +
          "`limit`. Use for pagination. Default 0.",
      ),
  })
  .strict();

export const getCaseStudiesTool = defineTool({
  name: "get_case_studies",
  description:
    "List + read case-studies from the playbook. Returns markdown bodies " +
    "sorted descending by key (newest month first). Call at the start of " +
    "a session to surface lessons from previous runs. Supports offset/" +
    "limit pagination plus an optional `month` filter.",
  inputSchema: GetCaseStudiesArgs,
  async execute(args, ctx) {
    const limit = args.limit ?? 20;
    const offset = args.offset ?? 0;
    const prefix = args.month
      ? `case-studies/${args.month}`
      : "case-studies";
    const keys = await ctx.storage.list(prefix);
    const sorted = keys.sort().reverse();
    const page = sorted.slice(offset, offset + limit);
    const studies = [];
    for (const key of page) {
      const body = await ctx.storage.load<string>(key);
      if (body) studies.push({ key, body });
    }
    return {
      count: studies.length,
      totalMatching: sorted.length,
      offset,
      nextOffset:
        offset + studies.length < sorted.length
          ? offset + studies.length
          : null,
      studies,
    };
  },
});
