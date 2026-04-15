import type { RawElement } from "../adapters/device-controller.port.js";

/**
 * Cross-platform "find the real editable input" strategy.
 *
 * In practice, text inputs in real apps are rendered as SIBLINGS of an
 * identity element (label / container with resourceId / contentDesc), NOT
 * as children of it. Two common patterns:
 *
 *   Pattern B (simple field, no icon):
 *     Parent
 *     ├── View_A  (resourceId, label)   ← anchor
 *     └── EditText                       ← target (direct sibling)
 *
 *   Pattern A (field with prefix/suffix icon — password toggle, clear, etc):
 *     Parent
 *     ├── View_A  (resourceId, label)    ← anchor
 *     └── View_B  (no info, container)   ← sibling
 *         ├── EditText                    ← target (descendant of sibling)
 *         └── Icon
 *
 * The anchor is what holds the stable identity (resourceId / contentDesc /
 * label). The EditText itself almost never has its own resourceId — we must
 * locate it structurally and report the ANCHOR's id as the stable reference.
 */

export interface InputQuery {
  resourceId?: string;
  contentDesc?: string;
  /** Match against `text` OR `contentDesc` OR `resourceId`. */
  label?: string;
  /**
   * Case-insensitive substring search across resourceId > contentDesc > text.
   * Preferred for cross-language matching — e.g. keyword="account" finds a
   * field with resourceId="account_number_field" even when the UI label is
   * Japanese. Priority: resourceId > contentDesc > text.
   */
  keyword?: string;
}

export type InputStrategy =
  | "following_sibling_edittext"
  | "following_sibling_container_edittext"
  | "descendant_edittext"
  | "self_is_edittext";

export interface StableId {
  source: "anchor.resourceId" | "anchor.contentDesc" | "anchor.label";
  value: string;
}

export interface InputMatch {
  /** The actual EditText / TextField / SecureTextField node to type into. */
  element: RawElement;
  /** How we located it from the anchor. */
  strategy: InputStrategy;
  /** The identity element (usually a label/container holding the resourceId). */
  anchor: RawElement;
  /** Best stable identifier derived from the anchor. */
  stableId: StableId | null;
}

/**
 * Cross-platform heuristic: is this element an editable text input?
 * Covers:
 *   - Android: EditText, AutoCompleteTextView, native subclasses
 *   - iOS: XCUIElementTypeTextField, XCUIElementTypeSecureTextField
 *   - Flutter / Compose: classes containing TextField, TextInput, Editable
 */
export function isEditText(el: RawElement): boolean {
  const cls = (el.className ?? "").toLowerCase();
  return (
    cls.includes("edittext") ||
    cls.includes("textfield") ||
    cls.includes("textinput") ||
    cls.includes("securetextfield") ||
    cls.includes("searchfield") ||
    cls.includes("editable")
  );
}

function matchesQuery(el: RawElement, q: InputQuery): boolean {
  if (q.resourceId && el.resourceId === q.resourceId) return true;
  if (q.contentDesc && el.contentDesc === q.contentDesc) return true;
  if (q.label) {
    if (el.text === q.label) return true;
    if (el.contentDesc === q.label) return true;
    if (el.resourceId === q.label) return true;
  }
  if (q.keyword) {
    const kw = q.keyword.toLowerCase();
    if ((el.resourceId ?? "").toLowerCase().includes(kw)) return true;
    if ((el.contentDesc ?? "").toLowerCase().includes(kw)) return true;
    if ((el.text ?? "").toLowerCase().includes(kw)) return true;
  }
  return false;
}

/** Depth-first walk that yields every node with its parent. */
function* walkWithParent(
  node: RawElement,
  parent: RawElement | null = null,
): IterableIterator<{ node: RawElement; parent: RawElement | null }> {
  yield { node, parent };
  for (const child of node.children ?? []) {
    yield* walkWithParent(child, node);
  }
}

function findDescendantEditText(node: RawElement): RawElement | null {
  for (const child of node.children ?? []) {
    if (isEditText(child)) return child;
    const deeper = findDescendantEditText(child);
    if (deeper) return deeper;
  }
  return null;
}

/**
 * Extract the best stable identifier from an anchor element. Priority:
 * resourceId > contentDesc > label (non-empty text).
 */
function stableIdOf(anchor: RawElement): StableId | null {
  if (anchor.resourceId && anchor.resourceId.trim()) {
    return { source: "anchor.resourceId", value: anchor.resourceId };
  }
  if (anchor.contentDesc && anchor.contentDesc.trim()) {
    return { source: "anchor.contentDesc", value: anchor.contentDesc };
  }
  if (anchor.text && anchor.text.trim()) {
    return { source: "anchor.label", value: anchor.text };
  }
  return null;
}

/**
 * Reverse of findInput: given an EditText, find its associated LABEL by
 * walking preceding-siblings and parent's prior children to find the nearest
 * element with non-empty resourceId / contentDesc / text. This matches the
 * common "label + field" layout regardless of order in a register/login
 * screen. Returns the stable id of that label element.
 */
export function labelForInput(
  root: RawElement,
  input: RawElement,
): { label?: string; stableId: StableId | null } {
  // Walk the tree to find the input's parent and its index among siblings.
  let foundParent: RawElement | null = null;
  let foundIdx = -1;
  for (const { node, parent } of walkWithParent(root)) {
    if (node === input && parent) {
      foundParent = parent;
      const kids = parent.children ?? [];
      foundIdx = kids.indexOf(input);
      break;
    }
  }
  if (!foundParent || foundIdx < 0) return { stableId: null };

  const siblings = foundParent.children ?? [];

  // Strategy A: preceding-sibling with any stable identity.
  for (let i = foundIdx - 1; i >= 0; i--) {
    const sib = siblings[i];
    const id = stableIdOf(sib);
    if (id) {
      return { label: sib.text ?? sib.contentDesc ?? sib.resourceId ?? undefined, stableId: id };
    }
    // Also look one level deep into the sibling in case the label is wrapped.
    for (const child of sib.children ?? []) {
      const cid = stableIdOf(child);
      if (cid) {
        return {
          label: child.text ?? child.contentDesc ?? child.resourceId ?? undefined,
          stableId: cid,
        };
      }
    }
  }

  // Strategy B: parent itself has a stable id.
  const parentId = stableIdOf(foundParent);
  if (parentId) {
    return {
      label: foundParent.text ?? foundParent.contentDesc ?? foundParent.resourceId ?? undefined,
      stableId: parentId,
    };
  }

  // Strategy C: walk up one more level (grandparent's descendants before parent).
  return { stableId: null };
}

/**
 * Collect every editable input in the tree along with the best semantic label
 * derived from its neighborhood. Result is sorted top-to-bottom by the
 * input's vertical position. Used by launch_app so agents can reference
 * fields by semantic label instead of positional index.
 */
export function collectInputs(
  root: RawElement,
): Array<{
  label?: string;
  stableId: StableId | null;
  center: { x: number; y: number };
  bounds: { left: number; top: number; right: number; bottom: number };
  currentValue?: string;
}> {
  const results: ReturnType<typeof collectInputs> = [];
  for (const { node } of walkWithParent(root)) {
    if (!isEditText(node)) continue;
    const b = node.bounds;
    if (!b) continue;
    const { label, stableId } = labelForInput(root, node);
    results.push({
      label,
      stableId,
      center: {
        x: Math.round((b.left + b.right) / 2),
        y: Math.round((b.top + b.bottom) / 2),
      },
      bounds: b,
      currentValue: node.text || undefined,
    });
  }
  results.sort((a, b) => a.bounds.top - b.bounds.top);
  return results;
}

/**
 * Run the strategy chain to locate the real EditText for a semantic query.
 * Strategy order (most → least common in practice):
 *
 *   1. following_sibling_edittext — anchor's next sibling IS the EditText
 *      (simple label + field row, no wrapper)
 *   2. following_sibling_container_edittext — anchor's next sibling is a
 *      container (typically no-info View) that holds the EditText plus
 *      icons/affordances (password toggle, clear button, prefix icon)
 *   3. descendant_edittext — anchor itself has an EditText descendant
 *   4. self_is_edittext — anchor IS the EditText (rare; requires explicit
 *      Semantics annotation on the input itself)
 *
 * First hit wins. Returns the EditText node + the anchor + strategy label +
 * best stable id derived from the anchor. Caller uses `element.bounds` /
 * center for typing (EditText itself almost never has a resourceId), and
 * caches `stableId` for subsequent identification without re-dumping.
 */
export function findInput(root: RawElement, query: InputQuery): InputMatch | null {
  const anchors: { node: RawElement; parent: RawElement | null }[] = [];
  for (const entry of walkWithParent(root)) {
    if (matchesQuery(entry.node, query)) anchors.push(entry);
  }
  if (anchors.length === 0) return null;

  for (const { node: anchor, parent } of anchors) {
    // Strategy 1 + 2: following siblings. Check each next sibling — if it's
    // an EditText directly (Pattern B), win. Otherwise look inside it for an
    // EditText descendant (Pattern A, wrapped with icons).
    if (parent) {
      const siblings = parent.children ?? [];
      const idx = siblings.indexOf(anchor);
      if (idx >= 0) {
        for (let i = idx + 1; i < siblings.length; i++) {
          const sib = siblings[i];
          if (isEditText(sib)) {
            return {
              element: sib,
              strategy: "following_sibling_edittext",
              anchor,
              stableId: stableIdOf(anchor),
            };
          }
          const sibDescendant = findDescendantEditText(sib);
          if (sibDescendant) {
            return {
              element: sibDescendant,
              strategy: "following_sibling_container_edittext",
              anchor,
              stableId: stableIdOf(anchor),
            };
          }
        }
      }
    }

    // Strategy 3: descendant of anchor itself.
    const descendant = findDescendantEditText(anchor);
    if (descendant) {
      return {
        element: descendant,
        strategy: "descendant_edittext",
        anchor,
        stableId: stableIdOf(anchor),
      };
    }

    // Strategy 4: anchor is the EditText. Rare — almost never happens in
    // practice because EditText nodes don't carry resourceId/contentDesc.
    if (isEditText(anchor)) {
      return {
        element: anchor,
        strategy: "self_is_edittext",
        anchor,
        stableId: stableIdOf(anchor),
      };
    }
  }
  return null;
}
