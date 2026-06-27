import type { ReactNode, ComponentType } from "react";

export type ToolWindowSide = "left" | "right" | "bottom";

export interface ToolWindowDescriptor {
  readonly id: string;
  readonly side: ToolWindowSide;
  readonly icon: ReactNode;
  readonly label: string;
  /**
   * True when the tool window is currently shown. Stripes use it
   * to highlight their button; for bottom-pane entries this also
   * reflects which sub-tab is active.
   */
  readonly isVisible: () => boolean;
  /**
   * Toggle the tool window. For bottom-pane entries this activates
   * the pane AND selects the sub-tab; clicking the same sub-tab
   * again hides the whole pane.
   */
  readonly toggle: () => void;
  /**
   * Body rendered inside the bottom pane when this descriptor is
   * the active sub-tab. Left / right tool windows ignore this —
   * their body is rendered by the shell directly.
   */
  readonly body?: ComponentType;
  /**
   * Optional badge count shown next to the label (e.g. error
   * count on Problems). Called on every render; keep cheap.
   */
  readonly badge?: () => number | null;
}

/**
 * Simple in-process registry. Features attach their tool windows
 * here at module load time; the shell reads the list once per
 * render. Insertion order wins ties within a side.
 *
 * Keeping this as a bare singleton array is deliberate — the shell
 * only needs a synchronous read and features only need a
 * synchronous push. Anything more ceremonial would be premature.
 */
const descriptors: ToolWindowDescriptor[] = [];

export const toolWindowRegistry = {
  register(desc: ToolWindowDescriptor): void {
    if (descriptors.some((d) => d.id === desc.id)) return;
    descriptors.push(desc);
  },
  all(): readonly ToolWindowDescriptor[] {
    return descriptors;
  },
  bySide(side: ToolWindowSide): readonly ToolWindowDescriptor[] {
    return descriptors.filter((d) => d.side === side);
  },
  byId(id: string): ToolWindowDescriptor | undefined {
    return descriptors.find((d) => d.id === id);
  },
} as const;
