import { z } from "zod";
import { defineTool, orchestraOrFail } from "../tool-definition.js";
import type { TreeNode } from "@atomyx/driver";

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
    includeSystemOverlays: z
      .boolean()
      .optional()
      .describe(
        "When true, include nodes belonging to OEM system overlays " +
          "(Samsung Edge Panel, OS-level navigation chrome). Default false: " +
          "agents care about the active app's surface, not the device shell.",
      ),
  })
  .strict();

/**
 * Resource-id prefixes belonging to OEM system overlays that ride
 * along on every accessibility tree dump but are never the agent's
 * target. Filtered out by default; pass `includeSystemOverlays: true`
 * when auditing the full tree.
 *
 *   - `com.samsung.android.app.cocktailbarservice` — Samsung Edge
 *     Panel trigger handle (always present on One UI devices).
 *
 * Add new prefixes here when a new OEM noise source is identified.
 */
const SYSTEM_OVERLAY_RESOURCE_PREFIXES: readonly string[] = [
  "com.samsung.android.app.cocktailbarservice:",
];

function isSystemOverlay(node: TreeNode): boolean {
  const id = node.attributes["id"];
  if (id === undefined) return false;
  for (const prefix of SYSTEM_OVERLAY_RESOURCE_PREFIXES) {
    if (id.startsWith(prefix)) return true;
  }
  return false;
}

interface FlatNode {
  role: string;
  id?: string;
  text?: string;
  label?: string;
  hint?: string;
  value?: string;
  bounds?: string;
  enabled?: boolean;
  clickable?: boolean;
  focused?: boolean;
  selected?: boolean;
  checked?: boolean;
  visible?: boolean;
  /**
   * Driver-specific attributes ("ext:ios-traits",
   * "ext:android-package", etc.). Only present when the driver
   * emitted at least one such key on the node. Cross-platform
   * agents should ignore this; debugger-style consumers (Studio
   * inspector, ad-hoc dump audits) read it to see the full
   * platform-native context behind a node's canonical role.
   */
  ext?: Record<string, string>;
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
    const tree = await orchestra.hierarchy({ signal: ctx.signal });
    const includeOverlays = args.includeSystemOverlays ?? false;
    const flat = flatten(tree, 0, includeOverlays);
    const limited = args.limit ? flat.slice(0, args.limit) : flat;
    return {
      total: flat.length,
      returned: limited.length,
      truncated: limited.length < flat.length,
      nodes: limited,
    };
  },
});

function flatten(
  node: TreeNode,
  depth: number,
  includeOverlays: boolean,
): FlatNode[] {
  const out: FlatNode[] = [];
  // Drop the entire overlay subtree, not just the matching node —
  // descendants of the trigger inherit no useful agent context.
  if (!includeOverlays && isSystemOverlay(node)) return out;
  let ext: Record<string, string> | undefined;
  for (const [key, value] of Object.entries(node.attributes)) {
    if (key.startsWith("ext:")) {
      (ext ??= {})[key] = value;
    }
  }
  out.push({
    role: node.attributes["role"] ?? "other",
    id: node.attributes["id"],
    text: node.attributes["text"],
    label: node.attributes["label"],
    hint: node.attributes["hint"],
    value: node.attributes["value"],
    bounds: node.attributes["bounds"],
    enabled: node.enabled,
    clickable: node.clickable,
    focused: node.focused,
    selected: node.selected,
    checked: node.checked,
    visible: node.visible,
    ext,
    depth,
  });
  for (const child of node.children) {
    out.push(...flatten(child, depth + 1, includeOverlays));
  }
  return out;
}
