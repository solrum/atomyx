import { z } from "zod";
import { defineTool, orchestraOrFail } from "../tool-definition.js";
import type { TreeNode } from "@atomyx/core-driver";

const GetUiTreeArgs = z
  .object({
    limit: z
      .number()
      .int()
      .positive()
      .optional()
      .describe(
        "Maximum number of nodes to include in the response. " +
          "Useful when the full tree is huge — pass e.g. 50 to get the top-N most relevant.",
      ),
  })
  .strict();

interface FlatNode {
  role: string;
  id?: string;
  text?: string;
  label?: string;
  bounds?: string;
  enabled?: boolean;
  clickable?: boolean;
  depth: number;
}

/**
 * `get_ui_tree` — agent-facing flat snapshot of the current
 * hierarchy. Returns a depth-annotated list of nodes with the
 * canonical attribute keys the agent works with. Avoids the
 * full nested tree because LLMs handle flat lists with
 * `depth` markers more reliably than recursive JSON.
 */
export const getUiTreeTool = defineTool({
  name: "get_ui_tree",
  description:
    "Capture the current UI hierarchy as a flat depth-annotated list of " +
    "elements. Each entry includes role, id, text, label, bounds, and " +
    "interactive state. THIS IS YOUR PRIMARY OBSERVATION TOOL — call it " +
    "FIRST after launch_app, after every navigation action (tap, back, " +
    "swipe), and whenever you need to understand the current screen. " +
    "Prefer this over screenshot — it is faster, cheaper, and gives you " +
    "actionable selectors for tap/input_text directly.",
  inputSchema: GetUiTreeArgs,
  async execute(args, ctx) {
    const orchestra = orchestraOrFail(ctx);
    const tree = await orchestra.hierarchy();
    const flat = flatten(tree, 0);
    const limited = args.limit ? flat.slice(0, args.limit) : flat;
    return {
      total: flat.length,
      returned: limited.length,
      truncated: limited.length < flat.length,
      nodes: limited,
    };
  },
});

function flatten(node: TreeNode, depth: number): FlatNode[] {
  const out: FlatNode[] = [];
  out.push({
    role: node.attributes["role"] ?? "other",
    id: node.attributes["id"],
    text: node.attributes["text"],
    label: node.attributes["label"],
    bounds: node.attributes["bounds"],
    enabled: node.enabled,
    clickable: node.clickable,
    depth,
  });
  for (const child of node.children) {
    out.push(...flatten(child, depth + 1));
  }
  return out;
}
