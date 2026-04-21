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
  enabled?: boolean;
  focused?: boolean;
  bounds?: { left: number; top: number; right: number; bottom: number };
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
 *   - `enabled` — direct from iOS `enabled` field.
 *   - `clickable` — derived from the interactive-type whitelist,
 *     since iOS snapshots don't expose an `isHittable` bit.
 */
export function normalizeIosTree(raw: IosRawElement): TreeNodeWire {
  const attributes: Record<string, string> = {};

  const elementType = raw.elementType ?? "other";
  attributes["class"] = elementType;
  attributes["role"] = iosElementTypeToRole(elementType);

  if (raw.identifier !== undefined && raw.identifier !== "") {
    attributes["id"] = raw.identifier;
  }
  if (raw.label !== undefined && raw.label !== "") {
    attributes["label"] = raw.label;
    attributes["text"] = raw.label;
  }
  if (raw.value !== undefined && raw.value !== "") {
    attributes["value"] = raw.value;
    // Override text with value when present — matches "what the
    // user sees as the current content" for text fields.
    attributes["text"] = raw.value;
  }
  if (raw.bounds) {
    attributes["bounds"] = formatBounds(raw.bounds);
  }

  const children: TreeNodeWire[] = (raw.children ?? []).map(normalizeIosTree);

  const node: TreeNodeWire = {
    attributes,
    children,
  };
  if (raw.enabled !== undefined) node.enabled = raw.enabled;
  if (raw.focused !== undefined) node.focused = raw.focused;
  node.clickable = INTERACTIVE_ROLES.has(elementType);

  return node;
}
