import { MirrorFrame } from "./mirror-frame.js";
import { InspectorPane } from "../ui-inspector/index.js";

interface MirrorInspectorPaneProps {
  /** When true, render the inspector tree on the right side. */
  readonly showInspector: boolean;
  /** Paint phone-style chrome around the mirror canvas (floating). */
  readonly chrome?: boolean;
  /** Hide MirrorPane's inline toolbar (stop / screenshot / aspect). */
  readonly hideMirrorToolbar?: boolean;
}

/**
 * Two-pane composite: live mirror on the left, UI inspector on the
 * right. Used by both the docked right-column slot and the floating
 * mirror window so they stay visually identical. When
 * `showInspector` is false the right pane collapses and the mirror
 * fills the available width.
 */
export function MirrorInspectorPane({
  showInspector,
  chrome = false,
  hideMirrorToolbar = false,
}: MirrorInspectorPaneProps) {
  return (
    <div
      style={{
        flex: 1,
        minWidth: 0,
        minHeight: 0,
        display: "flex",
        flexDirection: "row",
        gap: "var(--gap-3)",
        overflow: "hidden",
      }}
    >
      <div
        style={{
          flex: showInspector ? "0 0 360px" : "1 1 auto",
          minWidth: 0,
          minHeight: 0,
          display: "flex",
          flexDirection: "column",
          overflow: "hidden",
        }}
      >
        <MirrorFrame chrome={chrome} hideToolbar={hideMirrorToolbar} />
      </div>
      {showInspector ? (
        <div
          style={{
            flex: "1 1 auto",
            minWidth: 240,
            minHeight: 0,
            display: "flex",
            flexDirection: "column",
            borderLeft: "1px solid var(--line)",
            background: "var(--bg-1)",
          }}
        >
          <InspectorPane embedded />
        </div>
      ) : null}
    </div>
  );
}
