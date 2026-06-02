import { useEffect, useState, type CSSProperties } from "react";
import { Minimize2 } from "lucide-react";
import { TweaksPanel } from "../features/tweaks-panel/index.js";
import { FileTree } from "../features/workspace/index.js";
import { EditorPane } from "../features/editor/editor-pane.js";
import { Welcome } from "../features/welcome/index.js";
import "../features/settings/index.js";
import { SettingsView } from "../features/settings/index.js";
import "../features/command-palette/index.js";
import "../features/run-configs/index.js";
import { ActionPalette } from "../features/command-palette/index.js";
import { BottomToolWindow } from "../features/tool-windows/index.js";
import { ResizeHandle } from "../primitives/resize-handle.js";
import {
  RightStripe,
  BottomStripe,
} from "../features/tool-windows/index.js";
import { usePopups } from "../../state/features/popups/index.js";
import { NotificationStack } from "../features/notifications/index.js";
import { installDoubleShift } from "../features/command-palette/index.js";
import { installAppShellActions } from "./app-shell-actions.js";
import { PopupHost } from "./popup-host.js";
import { TitleBar } from "./title-bar.js";
import { StatusBar } from "./status-bar.js";
import { ActivityBar } from "./activity-bar.js";
import { RunDrawer } from "./run-drawer.js";
import { MirrorSlot } from "./mirror-slot.js";
import { DeviceMirrorWindow } from "../features/mirror-window/index.js";
import { useMirrorWindow } from "../../state/features/mirror-window/index.js";
import { POPUP_IDS } from "./popup-ids.js";
import { useEditor } from "../../state/features/editor/index.js";
import { useLayout } from "../../state/features/layout/index.js";
import { useSettings } from "../../state/features/settings/index.js";
import { useWorkspace } from "../../state/features/workspace/index.js";
import { cn } from "../primitives/index.js";

/**
 * Top-level shell. Layout maps onto the bundle's grid topology:
 *
 *   .app        rows  = 30px titlebar / 1fr workbench / 22px statusbar
 *   .workbench  cols  = 44px activity rail / 1fr panes / auto right stripe
 *   .panes      cols  = `--tree-w` file tree / 1fr editor / `--run-w` run pane
 *
 * Pane sizes flow from the layout store into CSS custom properties on
 * the `.panes` container; resize handles are absolutely positioned on
 * the cell borders and update those store values directly.
 */
export function AppShell() {
  const { currentPath } = useWorkspace();
  const { tabs } = useEditor();
  const { loaded, load } = useSettings();
  const layout = useLayout();
  const {
    fileTreeVisible,
    runDrawerVisible,
    runDrawerCollapsed,
    problemsVisible,
    zenMode,
    settingsViewVisible,
    paneSizes,
  } = layout;
  const popups = usePopups();
  const win = useMirrorWindow();
  const mirrorDocked = win.isOpen && win.dock === "right";
  const [tweaksOpen, setTweaksOpen] = useState(false);

  useEffect(() => {
    function onToggle() {
      setTweaksOpen((v) => !v);
    }
    window.addEventListener("atomyx:tweaks:toggle", onToggle);
    return () => window.removeEventListener("atomyx:tweaks:toggle", onToggle);
  }, []);

  useEffect(() => {
    if (!loaded) void load();
  }, [loaded, load]);

  useEffect(() => installAppShellActions(), []);

  useEffect(() => {
    installDoubleShift(() => popups.open(POPUP_IDS.findEverywhere));
  // popups is a stable registry singleton; the handler registers once at mount.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const overlays = (
    <>
      <DeviceMirrorWindow />
      <ActionPalette />
      <PopupHost />
      <NotificationStack />
      <TweaksPanel open={tweaksOpen} onClose={() => setTweaksOpen(false)} />
    </>
  );

  if (!currentPath && tabs.length === 0) {
    return (
      <>
        <Welcome onNewTest={() => popups.open(POPUP_IDS.wizard)} />
        {overlays}
      </>
    );
  }

  if (zenMode) {
    return (
      <div
        className="h-full flex flex-col relative"
        style={{ background: "var(--bg-0)", color: "var(--fg-0)" }}
      >
        <div className="flex-1 min-w-0 min-h-0">
          <EditorPane />
        </div>
        <button
          type="button"
          onClick={() => layout.toggleZen()}
          title="Exit Zen Mode — ⌘⌃F"
          aria-label="Exit Zen Mode"
          className="absolute bottom-3 right-3 flex items-center justify-center w-7 h-7 opacity-30 hover:opacity-100"
          style={{
            background: "var(--bg-1)",
            color: "var(--fg-0)",
            border: "1px solid var(--line)",
            borderRadius: "var(--r-2)",
          }}
        >
          <Minimize2 className="h-3.5 w-3.5" />
        </button>
        {overlays}
      </div>
    );
  }

  // Workbench needs a 3rd auto-sized column for the right stripe that
  // hosts tool-window stripes — design's `.workbench` ships with two
  // columns, so we override `grid-template-columns` here. The activity
  // rail and content area still resolve against the bundle classes.
  const workbenchStyle: CSSProperties = {
    gridTemplateColumns: "36px 1fr auto",
  };

  const panesStyle: CSSProperties = {
    flex: "1 1 auto",
    minHeight: 0,
    // Grid template comes from `.panes` rule; we drive sizing via vars.
    ["--tree-w" as string]: `${paneSizes.fileTreeWidth}px`,
    ["--right-w" as string]: `${paneSizes.mirrorWidth}px`,
    ["--run-h" as string]: `${paneSizes.runDrawerHeight}px`,
  };

  return (
    <div className="app">
      <TitleBar />
      <div className="workbench" style={workbenchStyle}>
        <ActivityBar />
        {settingsViewVisible ? (
          <div className="min-h-0 min-w-0 flex flex-col">
            <SettingsView />
          </div>
        ) : (
          <div className="flex flex-col min-h-0 min-w-0">
            <div
              className={cn(
                "panes",
                !fileTreeVisible && "no-tree",
                !mirrorDocked && "no-right",
                runDrawerVisible && "with-drawer",
                runDrawerVisible && runDrawerCollapsed && "drawer-collapsed",
              )}
              style={panesStyle}
            >
              <PaneCell
                visible={fileTreeVisible}
                style={{
                  background: "var(--bg-1)",
                  borderRight: "1px solid var(--line)",
                }}
              >
                <FileTree />
                <EdgeHandle
                  side="right"
                  orientation="vertical"
                  value={paneSizes.fileTreeWidth}
                  onChange={(v) => layout.setPaneSize("fileTreeWidth", v)}
                  onReset={() => layout.resetPaneSize("fileTreeWidth")}
                />
              </PaneCell>
              <div
                className="min-w-0 min-h-0"
                style={{ overflow: "hidden" }}
              >
                <EditorPane />
              </div>
              {mirrorDocked ? (
                <PaneCell visible className="mirror-cell">
                  <EdgeHandle
                    side="left"
                    orientation="vertical"
                    value={paneSizes.mirrorWidth}
                    onChange={(v) => layout.setPaneSize("mirrorWidth", v)}
                    onReset={() => layout.resetPaneSize("mirrorWidth")}
                    invert
                  />
                  <div className="right-col">
                    <MirrorSlot />
                  </div>
                </PaneCell>
              ) : (
                <PaneCell visible={false} className="mirror-cell" />
              )}
              {runDrawerVisible ? (
                <div className="drawer-cell relative min-w-0 min-h-0">
                  {!runDrawerCollapsed ? (
                    <EdgeHandle
                      side="top"
                      orientation="horizontal"
                      value={paneSizes.runDrawerHeight}
                      onChange={(v) =>
                        layout.setPaneSize("runDrawerHeight", v)
                      }
                      onReset={() =>
                        layout.resetPaneSize("runDrawerHeight")
                      }
                      invert
                    />
                  ) : null}
                  <RunDrawer />
                </div>
              ) : null}
            </div>
            {problemsVisible ? (
              <>
                <ResizeHandle
                  orientation="horizontal"
                  value={paneSizes.bottomPaneHeight}
                  onChange={(v) => layout.setPaneSize("bottomPaneHeight", v)}
                  onReset={() => layout.resetPaneSize("bottomPaneHeight")}
                  invert
                />
                <div
                  style={{
                    height: paneSizes.bottomPaneHeight,
                    flex: "0 0 auto",
                  }}
                  className="min-h-0"
                >
                  <BottomToolWindow />
                </div>
              </>
            ) : null}
            <BottomStripe />
          </div>
        )}
        <RightStripe />
      </div>
      <StatusBar />
      {overlays}
    </div>
  );
}

interface PaneCellProps {
  readonly visible: boolean;
  readonly style?: CSSProperties;
  readonly className?: string;
  readonly children?: React.ReactNode;
}

/**
 * Grid cell wrapper for the file-tree / mirror columns. The cell
 * is always laid out so `.panes.no-tree` / `.no-right` can collapse
 * its column to 0 via the bundle CSS, and contents only render
 * while visible — there is no point keeping the FileTree subscribed
 * to workspace events when it has 0 width.
 */
function PaneCell({ visible, style, className, children }: PaneCellProps) {
  return (
    <div
      className={cn("relative min-w-0 min-h-0", className)}
      style={{ overflow: "hidden", ...style }}
    >
      {visible ? children : null}
    </div>
  );
}

interface EdgeHandleProps {
  readonly side: "left" | "right" | "top" | "bottom";
  readonly orientation: "vertical" | "horizontal";
  readonly value: number;
  readonly onChange: (v: number) => void;
  readonly onReset?: () => void;
  readonly invert?: boolean;
}

/**
 * Absolutely-positioned drag affordance on a grid cell's edge.
 * Sits 4px across the seam (±2px overhang) so the pointer can
 * target the boundary between two grid cells without stealing
 * space from either cell.
 */
function EdgeHandle({
  side,
  orientation,
  value,
  onChange,
  onReset,
  invert,
}: EdgeHandleProps) {
  const placement: CSSProperties =
    side === "right"
      ? { right: -2, top: 0, bottom: 0, width: 4 }
      : side === "left"
        ? { left: -2, top: 0, bottom: 0, width: 4 }
        : side === "top"
          ? { top: -2, left: 0, right: 0, height: 4 }
          : { bottom: -2, left: 0, right: 0, height: 4 };
  return (
    <div
      style={{
        position: "absolute",
        display: "flex",
        zIndex: 2,
        ...placement,
      }}
    >
      <ResizeHandle
        orientation={orientation}
        value={value}
        onChange={onChange}
        onReset={onReset}
        invert={invert}
      />
    </div>
  );
}
