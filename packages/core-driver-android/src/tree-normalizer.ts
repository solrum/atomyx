import type { TreeNodeWire } from "@atomyx/core-driver-wire";

/**
 * Shape returned by the legacy Kotlin APK on `GET /tree` —
 * mirrors `RawElementDto` in the Android codebase. Fields match
 * Android AccessibilityNodeInfo semantics directly, which is why
 * this host-side adapter has to normalize into the canonical
 * `TreeNodeWire` shape before the core framework consumes it.
 *
 * Lifetime note: when the Kotlin APK migrates to expose the
 * canonical `/hierarchy` route, this normalizer shrinks or goes
 * away entirely. Until then, it IS the adapter layer —
 * everything the old TS adapter used to branch on platform for
 * now lives here as a pure translation function.
 */
export interface AndroidRawElement {
  elementId: string;
  className?: string;
  resourceId?: string;
  text?: string;
  contentDesc?: string;
  bounds?: { left: number; top: number; right: number; bottom: number };
  clickable?: boolean;
  enabled?: boolean;
  children?: AndroidRawElement[];
}

/**
 * Map the Android class name to a canonical normalized role.
 * The mapping is deliberately coarse — we only distinguish roles
 * that the cross-platform framework treats differently. Unknown
 * classes fall through to "other" so the attribute bag still has
 * a valid role value.
 *
 * The canonical role vocabulary matches
 * `@atomyx/core-driver/tree/tree-node.ts#Roles`. Duplicated here as
 * string literals because this package doesn't take a runtime
 * dependency on @atomyx/core-driver (dependency direction: drivers →
 * wire-schema only; drivers consume core types but through the
 * structural shape of `TreeNodeWire`, not by importing).
 */
export function classNameToRole(className: string | undefined): string {
  if (!className) return "other";
  const cls = className.toLowerCase();
  // Order matters: more specific substrings must be checked
  // before more generic ones. `radiobutton` / `imagebutton`
  // both contain "button" — test the specific names first.
  if (cls.includes("radiobutton")) return "radio-button";
  if (cls.includes("checkbox")) return "checkbox";
  if (cls.includes("edittext") || cls.includes("textinput")) return "text-field";
  if (cls.includes("searchview")) return "search-field";
  if (cls.includes("imagebutton")) return "button";
  if (cls.includes("button")) return "button";
  if (cls.includes("imageview")) return "image";
  if (cls.includes("textview")) return "text";
  if (cls.includes("switch")) return "switch";
  if (cls.includes("seekbar") || cls.includes("slider")) return "slider";
  if (cls.includes("recyclerview") || cls.includes("listview")) return "list";
  if (cls.includes("webview")) return "other";
  if (
    cls.includes("framelayout") ||
    cls.includes("linearlayout") ||
    cls.includes("relativelayout") ||
    cls.includes("constraintlayout") ||
    cls.includes("viewgroup") ||
    cls.includes("box") ||
    cls.includes("column") ||
    cls.includes("row")
  ) {
    return "container";
  }
  return "other";
}

/**
 * Serialize a bounds rect to the canonical "l,t,r,b" string
 * used by the wire schema and `@atomyx/core-driver/tree/bounds.ts`.
 */
function formatBounds(b: { left: number; top: number; right: number; bottom: number }): string {
  return `${b.left},${b.top},${b.right},${b.bottom}`;
}

/**
 * Translate a single Android `RawElementDto` into the canonical
 * `TreeNodeWire` shape. Recursive — walks the children array
 * once. Drops fields with no canonical counterpart (the Kotlin
 * `elementId` synthetic id is not carried over — consumers that
 * want node identity use cursor references from `@atomyx/core`).
 *
 * Attribute mapping (Android → canonical):
 *
 *   | Source                  | Target key in attributes |
 *   |-------------------------|--------------------------|
 *   | resourceId              | id                       |
 *   | contentDesc             | label                    |
 *   | text                    | text                     |
 *   | className               | class                    |
 *   | (derived from class)    | role                     |
 *   | bounds (DTO)            | bounds (string "l,t,r,b")|
 *
 * Boolean state (`clickable`, `enabled`) lands on the top-level
 * TreeNode fields, not inside the attribute bag — per the
 * wire-schema convention.
 */
export function normalizeAndroidTree(raw: AndroidRawElement): TreeNodeWire {
  const attributes: Record<string, string> = {};

  if (raw.resourceId !== undefined && raw.resourceId !== "") {
    attributes["id"] = raw.resourceId;
  }
  if (raw.contentDesc !== undefined && raw.contentDesc !== "") {
    attributes["label"] = raw.contentDesc;
  }
  if (raw.text !== undefined && raw.text !== "") {
    attributes["text"] = raw.text;
  }
  if (raw.className !== undefined && raw.className !== "") {
    attributes["class"] = raw.className;
  }
  attributes["role"] = classNameToRole(raw.className);
  if (raw.bounds) {
    attributes["bounds"] = formatBounds(raw.bounds);
  }

  const children: TreeNodeWire[] = (raw.children ?? []).map(normalizeAndroidTree);

  const node: TreeNodeWire = {
    attributes,
    children,
  };
  if (raw.clickable !== undefined) node.clickable = raw.clickable;
  if (raw.enabled !== undefined) node.enabled = raw.enabled;

  return node;
}
