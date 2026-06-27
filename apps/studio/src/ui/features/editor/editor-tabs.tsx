import { useEffect, useRef } from "react";
import {
  Pin,
  X,
  SplitSquareHorizontal,
  FileCode2,
  FileJson,
  FileText,
  FileMinus,
  File as FileIcon,
  Braces,
  Database,
  type LucideIcon,
} from "lucide-react";
import { cn, ContextMenu } from "../../primitives/index.js";
import {
  useEditor,
  type EditorGroup,
} from "../../../state/features/editor/index.js";

export interface EditorTabsProps {
  readonly group: EditorGroup;
  readonly canSplit: boolean;
  readonly canClose: boolean;
}

interface TabIconSpec {
  readonly Icon: LucideIcon;
  readonly color: string;
}

const TAB_ICON_BY_EXT: Readonly<Record<string, TabIconSpec>> = {
  yml: { Icon: FileCode2, color: "var(--syntax-yaml, #d19a66)" },
  yaml: { Icon: FileCode2, color: "var(--syntax-yaml, #d19a66)" },
  json: { Icon: Braces, color: "var(--syntax-json, #e5c07b)" },
  ts: { Icon: FileCode2, color: "var(--syntax-ts, #61afef)" },
  tsx: { Icon: FileCode2, color: "var(--syntax-tsx, #61afef)" },
  js: { Icon: FileCode2, color: "var(--syntax-js, #e5c07b)" },
  jsx: { Icon: FileCode2, color: "var(--syntax-jsx, #61afef)" },
  md: { Icon: FileText, color: "var(--fg-1)" },
  txt: { Icon: FileText, color: "var(--fg-2)" },
  log: { Icon: FileText, color: "var(--fg-3)" },
  sh: { Icon: FileCode2, color: "var(--syntax-sh, #98c379)" },
  rs: { Icon: FileCode2, color: "var(--syntax-rs, #e06c75)" },
  swift: { Icon: FileCode2, color: "var(--syntax-swift, #fa7343)" },
  kt: { Icon: FileCode2, color: "var(--syntax-kt, #c678dd)" },
  py: { Icon: FileCode2, color: "var(--syntax-py, #61afef)" },
  toml: { Icon: FileJson, color: "var(--fg-1)" },
  lock: { Icon: FileMinus, color: "var(--fg-3)" },
  stamp: { Icon: FileMinus, color: "var(--fg-3)" },
  filecache: { Icon: Database, color: "var(--fg-3)" },
};

const DEFAULT_TAB_ICON: TabIconSpec = {
  Icon: FileIcon,
  color: "var(--fg-2)",
};

function iconForPath(path: string): TabIconSpec {
  const name = path.split("/").pop() ?? path;
  if (name.startsWith(".")) {
    const stripped = name.slice(1);
    const hit = TAB_ICON_BY_EXT[stripped.toLowerCase()];
    if (hit) return hit;
  }
  const dot = name.lastIndexOf(".");
  if (dot < 0 || dot === name.length - 1) return DEFAULT_TAB_ICON;
  const ext = name.slice(dot + 1).toLowerCase();
  return TAB_ICON_BY_EXT[ext] ?? DEFAULT_TAB_ICON;
}

function disambiguate(
  paths: readonly string[],
): Readonly<Record<string, string | null>> {
  const basenames = new Map<string, number>();
  for (const p of paths) {
    const n = p.split("/").pop() ?? p;
    basenames.set(n, (basenames.get(n) ?? 0) + 1);
  }
  const out: Record<string, string | null> = {};
  for (const p of paths) {
    const segments = p.split("/");
    const name = segments.pop() ?? p;
    if ((basenames.get(name) ?? 0) <= 1) {
      out[p] = null;
      continue;
    }
    const parent = segments.pop();
    out[p] = parent ?? null;
  }
  return out;
}

export function EditorTabs({ group, canSplit, canClose }: EditorTabsProps) {
  const editor = useEditor();
  const scrollRef = useRef<HTMLDivElement | null>(null);

  // Translate vertical wheel into horizontal scroll on the tab
  // strip so a regular mouse can pan the tabs the same way a
  // trackpad can swipe them. Trackpads already report a non-zero
  // `deltaX` for horizontal swipes, so we only consume `deltaY`
  // — letting the native horizontal delta pass through unchanged.
  // Native listener (passive: false) because React's synthetic
  // wheel handler is registered passive and cannot preventDefault.
  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    const onWheel = (e: WheelEvent) => {
      if (e.deltaX !== 0 || e.deltaY === 0) return;
      e.preventDefault();
      el.scrollLeft += e.deltaY;
    };
    el.addEventListener("wheel", onWheel, { passive: false });
    return () => el.removeEventListener("wheel", onWheel);
  }, []);

  if (group.tabs.length === 0) return null;
  const activatePath = (path: string) => {
    editor.focusGroup(group.id);
    // activate mutates the active group, so focus first.
    editor.activate(path);
  };
  const closePath = (path: string) => {
    editor.focusGroup(group.id);
    editor.closeFile(path);
  };

  const parentByPath = disambiguate(group.tabs.map((t) => t.path));

  return (
    <div className="tabbar">
      <div ref={scrollRef} className="tabs-scroll">
        {group.tabs.map((tab) => {
          const active = tab.path === group.activePath;
          const name = tab.path.split("/").pop() ?? tab.path;
          const parent = parentByPath[tab.path];
          const { Icon, color } = iconForPath(tab.path);
          return (
            <ContextMenu
              key={tab.path}
              items={[
                {
                  id: "pin",
                  label: tab.pinned ? "Unpin Tab" : "Pin Tab",
                  onSelect: () => {
                    editor.focusGroup(group.id);
                    editor.togglePinned(tab.path);
                  },
                },
                {
                  id: "split-right",
                  label: "Split Right",
                  disabled: !canSplit,
                  onSelect: () => {
                    editor.focusGroup(group.id);
                    editor.activate(tab.path);
                    editor.splitRight();
                  },
                },
                "separator",
                {
                  id: "close",
                  label: "Close",
                  shortcut: "⌘W",
                  disabled: tab.pinned,
                  onSelect: () => closePath(tab.path),
                },
                {
                  id: "close-others",
                  label: "Close Other Tabs",
                  disabled: group.tabs.length <= 1,
                  onSelect: () => {
                    editor.focusGroup(group.id);
                    editor.closeOthers(tab.path);
                  },
                },
                {
                  id: "close-right",
                  label: "Close Tabs to the Right",
                  disabled:
                    group.tabs.findIndex((t) => t.path === tab.path) ===
                    group.tabs.length - 1,
                  onSelect: () => {
                    editor.focusGroup(group.id);
                    editor.closeToRight(tab.path);
                  },
                },
                "separator",
                {
                  id: "close-all",
                  label: "Close All Tabs",
                  danger: true,
                  onSelect: () => {
                    editor.focusGroup(group.id);
                    editor.closeAll();
                  },
                },
              ]}
            >
              {({ onContextMenu }) => (
                <div
                  onContextMenu={onContextMenu}
                  className={cn(
                    "tab",
                    active && "active",
                    tab.dirty && "dirty",
                  )}
                  onClick={() => activatePath(tab.path)}
                >
                  {tab.pinned ? (
                    <Pin
                      className="ficon"
                      style={{ color: "var(--accent)" }}
                    />
                  ) : (
                    <Icon className="ficon" style={{ color }} />
                  )}
                  <span className="tab-name truncate">{name}</span>
                  {parent ? (
                    <span className="tab-parent" title={tab.path}>
                      {parent}
                    </span>
                  ) : null}
                  <button
                    type="button"
                    onClick={(e) => {
                      e.stopPropagation();
                      if (tab.pinned) {
                        editor.focusGroup(group.id);
                        editor.togglePinned(tab.path);
                        return;
                      }
                      closePath(tab.path);
                    }}
                    className="x"
                    aria-label={tab.pinned ? `Unpin ${name}` : `Close ${name}`}
                  >
                    <X className="h-3 w-3" />
                  </button>
                </div>
              )}
            </ContextMenu>
          );
        })}
      </div>
      <div
        className="tabs-actions"
        style={{ gap: "var(--gap-3)", padding: "0 var(--gap-4)" }}
      >
        {canSplit ? (
          <button
            type="button"
            aria-label="Split right"
            title="Split Right"
            onClick={() => {
              editor.focusGroup(group.id);
              editor.splitRight();
            }}
            className="opacity-60 hover:opacity-100"
            style={{ color: "var(--fg-2)" }}
          >
            <SplitSquareHorizontal className="h-3 w-3" />
          </button>
        ) : null}
        {canClose ? (
          <button
            type="button"
            aria-label="Close split"
            title="Close Split"
            onClick={() => editor.closeGroup(group.id)}
            className="opacity-60 hover:opacity-100"
            style={{ color: "var(--fg-2)" }}
          >
            <X className="h-3 w-3" />
          </button>
        ) : null}
      </div>
    </div>
  );
}
