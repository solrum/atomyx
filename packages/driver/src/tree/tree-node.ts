/**
 * Canonical cross-platform UI element representation.
 *
 * `TreeNode` is a hierarchical snapshot — the tree shape is preserved,
 * but every per-platform metadata field is normalized into a single
 * string-keyed attribute bag. Drivers (iOS, Android, future Web) are
 * responsible for translating their native models into this shape at
 * the wire boundary; the core framework works exclusively on
 * `TreeNode` and never on platform-native types.
 *
 * Design decisions (frozen for 0.x, revisit before 1.0):
 *
 *   - `attributes` is a `Record<string, string>` rather than typed
 *     fields. Flexibility beats type safety here because the set of
 *     interesting attributes is open-ended and varies per platform
 *     (Android has resourceId / text / contentDescription; iOS has
 *     identifier / label / value; Web has id / textContent /
 *     aria-label). A typed field per concept would force the union
 *     of all platforms into one shape. A string bag with standardized
 *     keys (see {@link AttrKeys}) expresses the same intent with
 *     zero coupling.
 *
 *   - `TreeNode` itself is deeply readonly. Once a driver emits a
 *     tree, core treats it as an immutable snapshot — mutations
 *     would indicate a bug (stale data, accidental sharing between
 *     filter pipelines).
 *
 *   - State booleans (`clickable`, `enabled`, `focused`, `selected`,
 *     `checked`, `visible`) live as top-level optional fields rather
 *     than in the attribute bag. They are the only commonly-queried
 *     properties whose type is boolean across every platform, so
 *     giving them typed slots avoids forcing callers to parse
 *     `"true"` / `"false"` strings in a hot filter loop.
 *
 *   - `visible` reports accessibility-runtime visibility. Android
 *     reads `isVisibleToUser`, which accounts for offscreen scroll
 *     position. iOS currently reads frame ∩ window as a proxy;
 *     occlusion-aware detection via the daemon's
 *     `XC_kAXXCAttributeIsVisible` attribute is on the roadmap
 *     (symbol resolver wired, snapshot pre-fetch pending).
 */
export interface TreeNode {
  readonly attributes: Readonly<Record<string, string>>;
  readonly children: readonly TreeNode[];
  readonly clickable?: boolean;
  readonly enabled?: boolean;
  readonly focused?: boolean;
  readonly selected?: boolean;
  readonly checked?: boolean;
  readonly visible?: boolean;
}

/**
 * Canonical attribute keys. Every key here has cross-platform
 * semantics — drivers MUST populate them using their platform's
 * closest concept, NOT their platform's native field name.
 *
 * Key naming rule: pick from ARIA / HTML / a11y standards, never
 * from a platform SDK. `id` and `label`, not `resource-id` /
 * `content-desc` / `accessibility-label`.
 *
 * Platform → canonical mapping table (drivers implement this):
 *
 *   | Canonical | Android source                | iOS source                      |
 *   |-----------|-------------------------------|---------------------------------|
 *   | id        | resourceId                    | accessibilityIdentifier         |
 *   | text      | text                          | displayed value                 |
 *   | label     | contentDescription            | accessibilityLabel              |
 *   | hint      | hintText                      | placeholderValue                |
 *   | value     | input text, slider progress   | value                           |
 *   | role      | class → mapped role           | elementType → mapped role       |
 *   | class     | android.widget.Button         | XCUIElementTypeButton           |
 *   | package   | com.example.app               | com.example.App (bundleId)      |
 *   | bounds    | "l,t,r,b" in points           | "l,t,r,b" in points             |
 *
 * `class` and `package` are canonical but their VALUES are
 * platform-specific (different drivers emit different strings).
 * That's OK — the key is neutral (no branching in consumer code),
 * the value comparison is application-level logic.
 *
 * Any key not on this list is NON-PORTABLE. Drivers MAY emit
 * extra keys under the `ext:` prefix (see `ExtKeyPrefix`); consumers
 * read `ext:*` at their own risk. Drivers MUST NOT emit an `ext:`
 * key for a concept that has a canonical counterpart.
 */
export const AttrKeys = {
  /** Stable identifier — canonical cross-platform "id" concept. */
  Id: "id",
  /** Visible text content. */
  Text: "text",
  /** Accessibility label / description (ARIA-style). */
  Label: "label",
  /** Input placeholder or hint text. */
  Hint: "hint",
  /** Current value — input text, slider progress, switch state. */
  Value: "value",
  /** Normalized semantic role — see {@link Roles}. */
  Role: "role",
  /**
   * Element class — value is platform-specific raw class name.
   * Key is neutral so consumer code doesn't branch on platform;
   * value comparison is app-level logic ("if (attrs.class === ...)").
   */
  Class: "class",
  /** Containing app identifier — bundle id / package name. */
  Package: "package",
  /** Geometry as "left,top,right,bottom" in logical points. */
  Bounds: "bounds",
} as const;

/**
 * Escape hatch namespace for driver-specific attributes that have
 * no canonical equivalent. Emitted keys MUST start with this
 * prefix. Example: `ext:frame-rotation`, `ext:window-index`.
 *
 * Consumer rule: reading `ext:*` keys is explicitly non-portable.
 * Cross-platform code should never depend on `ext:*`; only code
 * that already knows which driver it's talking to should touch them.
 */
export const ExtKeyPrefix = "ext:" as const;

/**
 * Normalized semantic roles. Drivers map their native element
 * types into this enum; `role` in the attribute bag always holds
 * one of these values (or a string outside the enum for forward
 * compatibility — consumers should treat unknown roles as
 * `Other`, never error).
 */
export const Roles = {
  Button: "button",
  Link: "link",
  TextField: "text-field",
  SearchField: "search-field",
  SecureTextField: "secure-text-field",
  Cell: "cell",
  Image: "image",
  Icon: "icon",
  Switch: "switch",
  Slider: "slider",
  Checkbox: "checkbox",
  RadioButton: "radio-button",
  Text: "text",
  Heading: "heading",
  List: "list",
  Tab: "tab",
  Container: "container",
  Dialog: "dialog",
  Alert: "alert",
  Menu: "menu",
  Keyboard: "keyboard",
  Key: "key",
  Other: "other",
} as const;

export type Role = (typeof Roles)[keyof typeof Roles];

/**
 * Convenience accessor — pulls a canonical attribute by key,
 * returning `undefined` when missing. Exists so filter code doesn't
 * sprinkle `node.attributes[AttrKeys.Text]` everywhere.
 */
export function getAttr(node: TreeNode, key: string): string | undefined {
  return node.attributes[key];
}
