import type { ComponentType } from "react";
import type { PopupId } from "../../state/features/popups/index.js";

/**
 * A popup is a modal component that accepts `open` + `onClose`
 * and renders a centered overlay when open. Features attach their
 * popup components here at module load; the shell renders them
 * all once and uses the popups feature to decide which are
 * currently visible.
 */
export interface PopupDescriptor {
  readonly id: PopupId;
  readonly Component: ComponentType<{
    readonly open: boolean;
    readonly onClose: () => void;
  }>;
}

const descriptors: PopupDescriptor[] = [];

export const popupRegistry = {
  register(desc: PopupDescriptor): void {
    if (descriptors.some((d) => d.id === desc.id)) return;
    descriptors.push(desc);
  },
  all(): readonly PopupDescriptor[] {
    return descriptors;
  },
} as const;
