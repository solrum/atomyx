import { existsSync, mkdirSync, readFileSync, writeFileSync, appendFileSync } from "node:fs";
import { join } from "node:path";
import type { AdetContext } from "../runtime/adet-context.js";
import type { JsonSchema } from "../types.js";
import { Tool } from "./core/tool.js";
import { PLAYBOOK } from "./playbook-content.js";

// ── get_playbook ─────────────────────────────────────────────────────

export class GetPlaybookTool extends Tool<{
  args: Record<string, never>;
  result: { playbook: string };
}> {
  readonly name = "get_playbook";
  readonly description = "Tool-selection playbook. Call when unsure which tool to use.";
  readonly schema: JsonSchema = { type: "object", properties: {} };

  async execute() {
    return { playbook: PLAYBOOK };
  }
}

// ── add_case_study ────────────────────────────────────────────────────

export interface AddCaseStudyArgs {
  title: string;
  trigger: string;
  solution: string;
  example?: string;
}

export class AddCaseStudyTool extends Tool<{
  args: AddCaseStudyArgs;
  result: { ok: true; file: string };
}> {
  readonly name = "add_case_study";
  readonly description =
    "Append a learned lesson to .adet/case-studies/YYYY-MM.md.";
  readonly schema: JsonSchema = {
    type: "object",
    required: ["title", "trigger", "solution"],
    properties: {
      title: { type: "string" },
      trigger: { type: "string" },
      solution: { type: "string" },
      example: { type: "string" },
    },
  };

  async execute(args: AddCaseStudyArgs, _ctx: AdetContext) {
    const dir = join(process.cwd(), ".adet", "case-studies");
    mkdirSync(dir, { recursive: true });
    const d = new Date();
    const file = join(
      dir,
      `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}.md`,
    );
    const iso = d.toISOString();
    const entry =
      `\n## ${args.title}\n\n` +
      `**Logged:** ${iso}\n\n` +
      `**Trigger:** ${args.trigger}\n\n` +
      `**Solution:** ${args.solution}\n` +
      (args.example ? `\n**Example:**\n\`\`\`\n${args.example}\n\`\`\`\n` : "");
    if (!existsSync(file)) {
      writeFileSync(
        file,
        `# adet case studies — ${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}\n`,
      );
    }
    appendFileSync(file, entry);
    return { ok: true as const, file };
  }
}

// ── get_case_studies ──────────────────────────────────────────────────

export class GetCaseStudiesTool extends Tool<{
  args: { month?: string };
  result: { found: boolean; month: string; content?: string };
}> {
  readonly name = "get_case_studies";
  readonly description =
    "Read project case-studies. Call at session start for past gotchas.";
  readonly schema: JsonSchema = {
    type: "object",
    properties: {
      month: { type: "string" },
    },
  };

  async execute(args: { month?: string }) {
    const dir = join(process.cwd(), ".adet", "case-studies");
    const d = new Date();
    const month =
      args.month ?? `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}`;
    const file = join(dir, `${month}.md`);
    if (!existsSync(file)) {
      return { found: false, month };
    }
    return { found: true, month, content: readFileSync(file, "utf8") };
  }
}
