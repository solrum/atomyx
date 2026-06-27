import type { TreeNodeWire } from "@atomyx/driver-wire";

/**
 * Raw tree shape emitted by the Swift driver's `dumpRawTree`
 * command. Mirrors the JSON the Swift side produces from
 * `XCUIElementSnapshot` walks. Strings come through verbatim —
 * translation to canonical attribute keys happens here on the
 * host side, exactly once, at the wire boundary.
 *
 * Every field in `IosRawElement` plus the `normalizeIosTree` body
 * below together define the single mapping from the iOS snapshot
 * vocabulary to the canonical `TreeNodeWire` shape.
 */
export interface IosRawElement {
  elementType: string;
  identifier?: string;
  label?: string;
  value?: string;
  /** `XCUIElement.placeholderValue` — empty-field placeholder text. */
  placeholderValue?: string;
  enabled?: boolean;
  focused?: boolean;
  /**
   * On-screen flag computed Swift-side: frame ∩ window.frame
   * non-empty. Heuristic — does not catch occlusion by sibling
   * views or modal sheets. Maps to top-level `node.visible`.
   */
  visible?: boolean;
  /**
   * `isAccessibilityElement` — true when the element is an a11y
   * leaf (UIView with the flag set). False for pure container
   * views walked through during a11y traversal. iOS-only signal;
   * Android has no clean equivalent. Surfaced under
   * `attributes["ext:ios-accessible"]`.
   */
  accessible?: boolean;
  /**
   * `XCUIElementSnapshot.isSelected`. Set by tabs, segmented
   * controls, and any custom surface that calls
   * `accessibilityTraits.insert(.selected)`. Maps to top-level
   * `node.selected`.
   */
  selected?: boolean;
  /** `XCUIElementSnapshot.title`. Window / view title. */
  title?: string;
  /**
   * Decoded `UIAccessibilityTraits` bitmask. Strings match the
   * canonical UIAccessibility trait constants without the `UIA`
   * prefix (`button`, `image`, `staticText`, `link`, `searchField`,
   * `header`, `tabBar`, `selected`, `notEnabled`, `adjustable`,
   * `summaryElement`, `keyboardKey`, `causesPageTurn`,
   * `playsSound`, `startsMediaSession`, `allowsDirectInteraction`,
   * `updatesFrequently`). Survives Flutter's accessibility-node
   * merging that loses fidelity in `elementType` alone.
   */
  traits?: readonly string[];
  bounds?: { left: number; top: number; right: number; bottom: number };
  /**
   * `XCUIElementSnapshot.accessibilityFrame` when it differs from
   * `bounds`. Layout `bounds` describes the element's allocated
   * rectangle; this one describes its hit area.
   */
  accessibilityFrame?: { left: number; top: number; right: number; bottom: number };
  children?: IosRawElement[];
}

/**
 * Interactive iOS element types. Used to derive the `clickable`
 * boolean on the canonical node — iOS has no direct equivalent
 * to Android's `clickable` bit, so we whitelist known
 * interactive types.
 */
const INTERACTIVE_ROLES = new Set([
  "button",
  "cell",
  "link",
  "textField",
  "secureTextField",
  "searchField",
  "switch",
  "slider",
  "picker",
  "tab",
  "key",
]);

// Tuning knobs for the "merged Flutter / custom card" detector.
// See `isMergedSemanticLeaf` for the rationale of each cutoff.
const IMAGE_LABEL_MAX_CHARS = 40;
const MIN_MERGED_LINES = 3;
const SHORT_FIELD_AVG_CHARS = 30;
// A real iOS staticText leaf occupies one line of system font —
// observed at 21–44 px tall on iPhone 16 Pro Max screens. A
// Flutter-merged composite (icon-on-top + label-below in a card)
// reports as a single staticText leaf whose bounds height covers
// the full card region (commonly 80–150 px). Anything taller than
// MERGED_LEAF_MIN_HEIGHT_PX is treated as a merged composite and
// demoted to "container" so consumers can target the card region
// without thinking it is a literal text element.
//
// Why a height threshold and not a height-to-line-count ratio:
// the leaf carries no font-size signal and `label` is a single
// line of text in the merged case. We can't compute "expected
// height" without rendering. A flat px threshold is the simplest
// signal that distinguishes the two regimes from the data we
// actually have.
//
// Future enhancement: read `XC_kAXXCAttributeIsVisible` per element
// via XCAXClient_iOS to get true accessibility-runtime classification
// (UIKit knows whether the node is a leaf or a synthetic container).
// The integer keys are already resolved at agent load time
// (AccessibilityAttrSymbols); the per-node attribute query path is
// pending — `app.snapshot()` does not pre-fetch additionalAttributes.
const MERGED_LEAF_MIN_HEIGHT_PX = 70;

/**
 * Decide whether an iOS leaf with `elementType` and `label` is in
 * fact a merged composite (a Flutter card whose children were
 * collapsed by the accessibility engine) rather than a real image
 * or text element.
 *
 * The function ONLY returns true for leaves (`childCount === 0`).
 * If XCUITest reports descendants we trust the raw role — a real
 * container with children obviously isn't being merged at this
 * level.
 */
function isMergedSemanticLeaf(
  elementType: string,
  label: string | undefined,
  childCount: number,
  bounds:
    | { left: number; top: number; right: number; bottom: number }
    | undefined,
): boolean {
  if (childCount > 0) return false;
  // Bounds-driven detection runs first because it works without
  // a label — Flutter cards often have a non-empty label but the
  // discriminator is geometric, not textual. Apply for both
  // staticText (icon-on-top + text-below pattern) and image (pure
  // icon card with no label) since either can be a merged card.
  if (
    (elementType === "staticText" || elementType === "image") &&
    bounds !== undefined
  ) {
    const heightPx = bounds.bottom - bounds.top;
    if (heightPx >= MERGED_LEAF_MIN_HEIGHT_PX) return true;
  }
  if (label === undefined || label === "") return false;
  const newlineCount = (label.match(/\n/g) ?? []).length;

  if (elementType === "image") {
    return label.length > IMAGE_LABEL_MAX_CHARS || newlineCount >= 1;
  }

  if (elementType === "staticText") {
    if (newlineCount < MIN_MERGED_LINES) return false;
    const avgLineLength = label.length / (newlineCount + 1);
    return avgLineLength < SHORT_FIELD_AVG_CHARS;
  }

  return false;
}

/**
 * Map iOS `XCUIElement.ElementType` string to a canonical role.
 *
 * iOS element types are already semantic in the way cross-
 * platform roles should be, so most entries are direct renames
 * (e.g. `"textField"` → `"text-field"`). The translation here is
 * mostly hyphenation plus a few consolidations:
 *
 *   - `staticText` → `"text"` (iOS sometimes calls display text
 *     "static text" to distinguish from editable fields).
 *   - `scrollView` / `collectionView` / `table` → `"container"`
 *     (cross-platform consumers don't care about the specific
 *     container type; the `class` attribute keeps the original).
 *   - Unknown types fall through to `"other"` so consumers can
 *     still reason about them via `attributes.class`.
 */
export function iosElementTypeToRole(elementType: string): string {
  switch (elementType) {
    case "button":
    case "popupButton":
      return "button";
    case "link":
      return "link";
    case "textField":
      return "text-field";
    case "secureTextField":
      return "secure-text-field";
    case "searchField":
      return "search-field";
    case "staticText":
      return "text";
    case "cell":
      return "cell";
    case "image":
    case "icon":
      return "image";
    case "switch":
    case "toggle":
      return "switch";
    case "slider":
      return "slider";
    case "checkBox":
      return "checkbox";
    case "radioButton":
      return "radio-button";
    case "navigationBar":
    case "tabBar":
    case "tabGroup":
    case "toolbar":
    case "menuBar":
    case "menu":
      return "menu";
    case "alert":
      return "alert";
    case "dialog":
    case "sheet":
      return "dialog";
    case "keyboard":
      return "keyboard";
    case "key":
      return "key";
    case "table":
    case "collectionView":
    case "scrollView":
    case "group":
    case "window":
    case "splitGroup":
    case "stackView":
    case "layoutArea":
      return "container";
    case "heading":
      return "heading";
    case "pickerWheel":
    case "picker":
      return "slider";
    // Loading indicators. These role values are consumed by the
    // cross-platform transition classifier to detect "screen is
    // still loading"; without them iOS activity / progress
    // indicators would fall through to "other" and be invisible
    // to the classifier.
    case "activityIndicator":
      return "activityindicator";
    case "progressIndicator":
      return "progressindicator";
    case "other":
      return "other";
    default:
      return "other";
  }
}

function formatBounds(b: { left: number; top: number; right: number; bottom: number }): string {
  return `${b.left},${b.top},${b.right},${b.bottom}`;
}

/**
 * Translate one iOS `IosRawElement` into canonical `TreeNodeWire`.
 *
 * Attribute mapping (iOS → canonical):
 *
 *   | Source                          | Target                       |
 *   |---------------------------------|------------------------------|
 *   | identifier                      | attributes.id                |
 *   | label                           | attributes.label             |
 *   | label (mirrored when no value)  | attributes.text              |
 *   | value                           | attributes.value             |
 *   | value (overrides text when set) | attributes.text              |
 *   | placeholderValue                | attributes.hint              |
 *   | elementType                     | attributes.class             |
 *   | (derived from elementType)      | attributes.role              |
 *   | bounds                          | attributes.bounds            |
 *
 * Why `label` is mirrored into `text`: agents that use
 * cross-platform selectors pass `{text: "Login"}` expecting to
 * find a button labeled "Login". On Android, the button's
 * visible text goes into the `text` attribute. On iOS there is
 * no `text` field — the visible content is exposed via the
 * accessibility label. Mirroring keeps cross-platform selector
 * matches working without requiring the agent to know iOS
 * a11y semantics. When `value` is present (text field with
 * content), we override `text` with `value` — that matches
 * what an agent would see as "current content" of an input.
 *
 * State booleans:
 *
 *   - `enabled`, `focused`, `selected`, `visible` — direct from
 *     the corresponding iOS fields when present.
 *   - `clickable` — derived: trait `button` / `link` is the
 *     strongest signal, otherwise fall back to the interactive-
 *     type whitelist. iOS snapshots don't expose an `isHittable`
 *     bit on snapshots (only on live `XCUIElement` queries).
 *   - `visible` — Swift bridge fills it from a frame ∩ window
 *     intersect; does not catch occlusion. Reading
 *     `XC_kAXXCAttributeIsVisible` via dlsym would be richer; the
 *     symbol resolver is wired but snapshot pre-fetch is pending.
 */
export function normalizeIosTree(raw: IosRawElement): TreeNodeWire {
  const attributes: Record<string, string> = {};

  const elementType = raw.elementType ?? "other";
  attributes["class"] = elementType;

  // Pick the canonical role. Decision order, highest confidence
  // first:
  //
  //   1. `keyboard` elementType always wins — `Orchestra`'s
  //      keyboard-dismiss gate hard-codes `role === "keyboard"`,
  //      and the keyboard root is reliably reported as that
  //      elementType.
  //   2. Accessibility traits, when the agent supplied them: the
  //      bitmask survives Flutter's accessibility-node merging
  //      that disguises a card as a single `image` or `staticText`
  //      leaf. See `traitsToRole` for the mapping table.
  //   3. Raw `elementType` via `iosElementTypeToRole`.
  //   4. Last-resort label-shape heuristic for `image` /
  //      `staticText` leaves that lack traits — mainly relevant
  //      when running against older iOS / agent builds that don't
  //      yet emit traits.
  let role: string;
  if (elementType === "keyboard") {
    role = "keyboard";
  } else {
    const fromTraits = raw.traits ? traitsToRole(raw.traits) : null;
    if (fromTraits !== null) {
      role = fromTraits;
    } else if (
      isMergedSemanticLeaf(
        elementType,
        raw.label,
        raw.children?.length ?? 0,
        raw.bounds,
      )
    ) {
      role = "container";
    } else {
      role = iosElementTypeToRole(elementType);
    }
    // Trait says "text" / "image" but bounds dwarf a real one-line
    // text leaf — Flutter and other custom UIKit composites
    // collapse a card (icon + label inside one tappable region)
    // into a single accessibility leaf whose trait set is either
    // ["staticText"] alone or ["image"] alone. The trait cannot
    // distinguish a real leaf from a merged card; the height does.
    if (
      (role === "text" || role === "image") &&
      isMergedSemanticLeaf(
        elementType,
        raw.label,
        raw.children?.length ?? 0,
        raw.bounds,
      )
    ) {
      role = "container";
    }
  }
  attributes["role"] = role;

  if (raw.identifier !== undefined && raw.identifier !== "") {
    attributes["id"] = raw.identifier;
  }
  // Route the raw a11y label by the INFERRED role:
  //
  //   - true text leaf (`role === "text"` or `"heading"`) → the
  //     label IS the visible text; expose as `attributes.text`.
  //   - everything else → keep as `attributes.label`. Buttons,
  //     views, and merged composites carry an a11y description,
  //     not visible text content. Consumers can tell "container
  //     with a11y description" apart from "text leaf with a
  //     value".
  //
  // Cross-platform `{text: "..."}` selectors fall back to `label`
  // via `compileSelector`'s priority broadening, so the missing
  // mirror does not break iOS button matches.
  if (raw.label !== undefined && raw.label !== "") {
    if (role === "text" || role === "heading") {
      attributes["text"] = raw.label;
    } else {
      attributes["label"] = raw.label;
    }
  }
  if (raw.value !== undefined && raw.value !== "") {
    attributes["value"] = raw.value;
    // Override text with value when present — matches "what the
    // user sees as the current content" for text fields.
    attributes["text"] = raw.value;
  }
  if (raw.placeholderValue !== undefined && raw.placeholderValue !== "") {
    attributes["hint"] = raw.placeholderValue;
  }
  if (raw.bounds) {
    attributes["bounds"] = formatBounds(raw.bounds);
  }
  if (raw.title !== undefined && raw.title !== "") {
    attributes["ext:ios-title"] = raw.title;
  }
  if (raw.traits && raw.traits.length > 0) {
    attributes["ext:ios-traits"] = raw.traits.join(",");
  }
  if (raw.accessibilityFrame) {
    attributes["ext:ios-a11y-bounds"] = formatBounds(raw.accessibilityFrame);
  }
  if (raw.accessible !== undefined) {
    attributes["ext:ios-accessible"] = String(raw.accessible);
  }

  const children: TreeNodeWire[] = (raw.children ?? []).map(normalizeIosTree);

  const node: TreeNodeWire = {
    attributes,
    children,
  };
  if (raw.enabled !== undefined) node.enabled = raw.enabled;
  if (raw.focused !== undefined) node.focused = raw.focused;
  if (raw.selected !== undefined) node.selected = raw.selected;
  if (raw.visible !== undefined) node.visible = raw.visible;
  // Clickable derivation: trait `button` / `link` is the strongest
  // signal — a Flutter button merged into a `staticText` leaf is
  // still tappable, and the trait carries that. Otherwise fall
  // back to the elementType whitelist for legacy / no-trait dumps.
  node.clickable =
    (raw.traits?.includes("button") ?? false) ||
    (raw.traits?.includes("link") ?? false) ||
    INTERACTIVE_ROLES.has(elementType);

  return node;
}

/**
 * Map a decoded `UIAccessibilityTraits` set to a canonical role.
 * Returns `null` when the trait set is empty or carries no
 * role-defining bits — caller falls back to the elementType
 * mapping in that case.
 *
 * Decision priority (most specific first):
 *
 *   • `searchField`              → `search-field`
 *   • `keyboardKey`              → `key`
 *   • `header`                   → `heading`
 *   • `link`                     → `link`
 *   • `button` AND `image`       → `container` (visual button +
 *                                   image often signals a card)
 *   • `button`                   → `button`
 *   • `image` AND `staticText`   → `container` (Flutter merge of
 *                                   icon + text into one leaf)
 *   • `image`                    → `image`
 *   • `staticText`               → `text`
 *
 * The "two content traits → container" rule is the precise signal
 * we lacked when only the label shape was available: traits are
 * decided by Apple's accessibility runtime from the underlying
 * semantics graph and cannot be confused by manual `\n` in a real
 * label.
 */
function traitsToRole(traits: readonly string[]): string | null {
  if (traits.length === 0) return null;
  const has = (t: string) => traits.includes(t);
  if (has("searchField")) return "search-field";
  if (has("keyboardKey")) return "key";
  if (has("header")) return "heading";
  if (has("link")) return "link";
  if (has("button") && has("image")) return "container";
  if (has("button")) return "button";
  if (has("image") && has("staticText")) return "container";
  if (has("image")) return "image";
  if (has("staticText")) return "text";
  return null;
}
