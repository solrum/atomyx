import { useEffect } from "react";
import { Atom } from "lucide-react";
import { RuntimeStatusIndicator } from "../features/runtime-status/index.js";
import { useEditor } from "../../state/features/editor/index.js";
import { useWorkspace } from "../../state/features/workspace/index.js";
import { useLayout, type ViewMode } from "../../state/features/layout/index.js";
import { DevicePicker } from "./device-picker.js";

interface ModeDef {
  readonly id: ViewMode;
  readonly label: string;
  readonly kbd: string;
  readonly cls: "" | "live" | "debug";
}

const MODES: readonly ModeDef[] = [
  { id: "author", label: "Author", kbd: "⌘1", cls: "" },
  { id: "live", label: "Live", kbd: "⌘2", cls: "live" },
  { id: "debug", label: "Debug", kbd: "⌘3", cls: "debug" },
];

function basename(path: string): string {
  const parts = path.split(/[\\/]/).filter(Boolean);
  return parts[parts.length - 1] ?? path;
}

/**
 * Top window strip. On macOS with `titleBarStyle: "Overlay"` the
 * native traffic lights render inside the first 78px — the padding
 * below reserves that space. The whole strip is a drag region via
 * `data-tauri-drag-region`; interactive children opt out.
 *
 * Hosts the Author/Live/Debug mode switcher; the active mode is
 * mirrored onto `<html data-mode>` so ambient tints (`.center-col`
 * top-edge bar) and right-column slot accents activate via CSS.
 */
export function TitleBar() {
  const { currentPath } = useWorkspace();
  const { tabs } = useEditor();
  const { viewMode, setViewMode } = useLayout();
  const dirtyCount = tabs.filter((t) => t.dirty).length;
  const projectName = currentPath ? basename(currentPath) : null;

  useEffect(() => {
    document.documentElement.setAttribute("data-mode", viewMode);
  }, [viewMode]);

  return (
    <div
      data-tauri-drag-region="true"
      className="titlebar select-none"
      style={{ paddingLeft: "78px" }}
    >
      <div
        data-tauri-drag-region="true"
        className="brand"
        style={{ flex: 1 }}
      >
        <Atom
          data-tauri-drag-region="true"
          style={{ width: "12px", height: "12px", color: "var(--accent)" }}
        />
        <span data-tauri-drag-region="true">Atomyx Studio</span>
      </div>
      <div
        data-tauri-drag-region="true"
        className="title flex items-center"
        style={{ gap: "4px", justifyContent: "center" }}
      >
        <span
          data-tauri-drag-region="true"
          className="truncate"
          style={{ color: "var(--fg-0)" }}
          title={currentPath ?? "Atomyx Studio"}
        >
          {projectName ?? "Atomyx Studio"}
        </span>
        {dirtyCount > 0 ? (
          <span
            data-tauri-drag-region="true"
            aria-label={`${dirtyCount} unsaved changes`}
            title={`${dirtyCount} unsaved change${dirtyCount === 1 ? "" : "s"}`}
            style={{
              width: "6px",
              height: "6px",
              borderRadius: "50%",
              background: "var(--accent)",
              flex: "0 0 auto",
            }}
          />
        ) : null}
      </div>
      <div
        data-tauri-drag-region="true"
        className="flex items-center justify-end"
        style={{ flex: 1, gap: "var(--gap-3)" }}
      >
        <div
          data-tauri-drag-region="false"
          className="mode-switcher"
          role="tablist"
          aria-label="View mode"
        >
          {MODES.map((m) => (
            <button
              key={m.id}
              type="button"
              className={`ms-btn ${m.cls} ${viewMode === m.id ? "active" : ""}`}
              role="tab"
              aria-selected={viewMode === m.id}
              onClick={() => setViewMode(m.id)}
              title={`${m.label} mode (${m.kbd})`}
            >
              <span className="ms-dot" />
              {m.label}
              <span className="ms-kbd">{m.kbd}</span>
            </button>
          ))}
        </div>
        <div
          data-tauri-drag-region="false"
          className="flex items-center"
          style={{ gap: "var(--gap-3)" }}
        >
          <DevicePicker />
          <RuntimeStatusIndicator />
        </div>
      </div>
    </div>
  );
}
