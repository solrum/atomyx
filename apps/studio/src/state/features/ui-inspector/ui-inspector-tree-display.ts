import type { UiTreeNode } from "../../../domain/features/runtime/index.js";

/**
 * Pure rendering helpers for the inspector tree row. Live in the
 * state layer so the UI component is JSX + event wiring only and
 * the label-formatting rules are unit-testable under `node:test`.
 */

/**
 * Build the one-line label shown next to a tree row. Picks the
 * most informative attribute available — text > label > id — and
 * falls back to the bare role/class when none are present.
 *
 * The canonical cross-platform `role` is preferred over the raw
 * platform `class`. iOS XCUITest reports a Flutter-merged button
 * as a `staticText` element whose frame still covers the entire
 * tappable region; surfacing the role + a `[click]` marker on
 * `node.clickable` keeps the tree readable when the elementType
 * doesn't match the visual affordance.
 *
 * Format examples:
 *   - `text "Welcome"`               (passive text)
 *   - `text "Tất cả" [click]`        (Flutter button surfaced as text)
 *   - `button (Sign in)`             (label, no text)
 *   - `webView #login-frame`         (id, no text/label)
 *   - `View`                         (Android Flutter Semantics view)
 *
 * When the canonical role is "other" (the cross-platform vocabulary
 * had no match for this element), fall back to the raw `class` so
 * the row carries something the developer can grep for — Android
 * `android.view.View` collapses to just `View`, and an iOS class
 * already renders short. Without the fallback every Flutter
 * Semantics view on Android would render as the literal string
 * "other", which is visually identical regardless of what the node
 * actually is.
 */
export function summarize(node: UiTreeNode, showRaw = false): string {
  const attrs = node.attributes;
  const cls = attrs["class"];
  const rawRole = attrs["role"];
  const role =
    rawRole !== undefined && rawRole !== "other"
      ? rawRole
      : (shortenClass(cls) ?? rawRole ?? "node");
  const id = attrs["id"];
  const text = attrs["text"];
  const label = attrs["label"];
  const tag =
    text !== undefined
      ? `"${truncate(text, 40)}"`
      : label !== undefined
        ? `(${truncate(label, 40)})`
        : id !== undefined
          ? `#${id}`
          : "";
  const clickMark = node.clickable && role !== "button" ? " [click]" : "";
  const rawTail = showRaw && cls !== undefined && cls !== role ? ` · ${cls}` : "";
  const head = tag ? `${role} ${tag}` : role;
  return `${head}${clickMark}${rawTail}`;
}

/**
 * Truncate `s` to at most `max` characters, replacing the tail
 * with an ellipsis when shortened. Strings already within budget
 * are returned unchanged.
 */
export function truncate(s: string, max: number): string {
  if (s.length <= max) return s;
  return s.slice(0, max - 1) + "…";
}

/**
 * Strip a fully-qualified Android class to its last segment so the
 * tree row stays scannable: `android.view.View` → `View`,
 * `android.widget.LinearLayout` → `LinearLayout`. iOS classes
 * (`button`, `staticText`) carry no dots and pass through unchanged.
 * Returns `undefined` when the input is empty or absent.
 */
function shortenClass(cls: string | undefined): string | undefined {
  if (cls === undefined || cls === "") return undefined;
  const idx = cls.lastIndexOf(".");
  return idx >= 0 ? cls.slice(idx + 1) : cls;
}
