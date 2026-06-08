import type { SelectorCandidate, UiTreeNode } from "./script-actions.types.js";

/**
 * Escape a value for safe inclusion inside a double-quoted YAML
 * scalar. Only the backslash and double-quote need escaping — other
 * characters pass through so the generated step is readable.
 */
function yamlQuote(value: string): string {
  return `"${value.replace(/\\/g, "\\\\").replace(/"/g, '\\"')}"`;
}

/**
 * Render a simple-attribute selector candidate as the shortest
 * valid YAML shorthand the schema accepts — `tap: "Sign in"` /
 * `tap: { id: "..." }` — so generated steps stay readable instead
 * of exploding into five-line block selectors.
 *
 *   - text kind: bare quoted scalar. The schema expands that to
 *     both `text` and `label`, matching via priority broadening.
 *   - id / label / hint: flow mapping with the explicit field so
 *     the selector says exactly what the author picked.
 */
function shorthandFor(kind: string, value: string): string {
  if (kind === "text") return yamlQuote(value);
  return `{ ${kind}: ${yamlQuote(value)} }`;
}

/**
 * Roles the `role + nth` fallback will use when a node has no
 * stable attribute. Generic roles like "container" or "other" are
 * excluded — they'd match everything and the `nth` index would
 * be unusable in practice. Each entry is a value the Android /
 * iOS tree normalisers can emit via `classNameToRole` /
 * `iosElementTypeToRole`.
 */
const USEFUL_ROLES: ReadonlySet<string> = new Set([
  "text-field",
  "button",
  "checkbox",
  "switch",
  "image",
  "link",
  "menu-item",
  "tab",
  "cell",
]);

/**
 * Depth-first index of `target` among every node whose
 * `attributes.role` equals `role`. Returns `-1` when `target` is
 * not reachable from `root` (which happens in tests; the inspector
 * always passes a tree that contains the node).
 */
function indexOfNodeByRole(
  root: UiTreeNode,
  target: UiTreeNode,
  role: string,
): number {
  let count = -1;
  let seen = false;
  const walk = (node: UiTreeNode): boolean => {
    if (node.attributes["role"] === role) count += 1;
    if (node === target) {
      seen = true;
      return true;
    }
    for (const child of node.children) {
      if (walk(child)) return true;
    }
    return false;
  };
  walk(root);
  return seen ? count : -1;
}

/**
 * Build a prioritised list of selector candidates from a tree node.
 * Order matches `compileSelector` broadening in `@atomyx/driver`
 * so consumers that pick the first entry get the most-stable
 * selector by default.
 *
 * When the node carries no stable attribute AND `tree` is supplied
 * AND its role is in `USEFUL_ROLES`, a last-resort `role + nth`
 * candidate lands in the list: `{ role: "text-field", nth: 2 }`.
 * Index is computed by depth-first traversal over all nodes with
 * the same role — brittle under layout changes, but authorable
 * from the inspector when the app exposes no identifiers at all.
 */
export function selectorsFromNode(
  node: UiTreeNode,
  tree?: UiTreeNode,
): readonly SelectorCandidate[] {
  const out: SelectorCandidate[] = [];
  const { attributes } = node;

  const push = (kind: SelectorCandidate["kind"], value: string) => {
    out.push({
      kind,
      display: `${kind} "${value}"`,
      toYamlInline: () => shorthandFor(kind, value),
      toYamlBlockLines: () => [`${kind}: ${yamlQuote(value)}`],
    });
  };

  if (attributes["id"]) push("id", attributes["id"]);
  if (attributes["label"]) push("label", attributes["label"]);
  if (attributes["text"]) push("text", attributes["text"]);
  if (attributes["hint"]) push("hint", attributes["hint"]);

  if (out.length === 0 && tree) {
    const role = attributes["role"];
    if (role && USEFUL_ROLES.has(role)) {
      const idx = indexOfNodeByRole(tree, node, role);
      if (idx >= 0) {
        out.push({
          kind: "role-nth",
          display: `role "${role}" #${idx}`,
          toYamlInline: () =>
            `{ role: ${yamlQuote(role)}, nth: ${idx} }`,
          toYamlBlockLines: () => [
            `role: ${yamlQuote(role)}`,
            `nth: ${idx}`,
          ],
        });
      }
    }
  }

  return out;
}

export function bestSelector(
  candidates: readonly SelectorCandidate[],
): SelectorCandidate | null {
  return candidates[0] ?? null;
}
