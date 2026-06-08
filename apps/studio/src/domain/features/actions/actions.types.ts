/**
 * Generic action metadata consumed by the action palette, the
 * keymap dispatcher, and any future toolbar / menu. Pure data —
 * executors live in the state layer so actions can call into
 * stores without the domain importing them.
 */

export interface KeyMatcher {
  readonly key: string;
  readonly meta?: boolean;
  readonly ctrl?: boolean;
  readonly shift?: boolean;
  readonly alt?: boolean;
}

export interface ActionDefinition {
  readonly id: string;
  readonly label: string;
  readonly category: string;
  /**
   * Human-readable shortcut string shown in the palette and
   * menus, e.g. "⌘S" or "⌘⇧A".
   */
  readonly shortcut?: string;
  /**
   * Structured key matcher used by the global keymap dispatcher.
   * Absence = action is only runnable from the palette.
   */
  readonly keyMatcher?: KeyMatcher;
}
