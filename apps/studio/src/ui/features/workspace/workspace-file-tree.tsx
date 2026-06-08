import { useCallback, useMemo, useState } from "react";
import {
  ChevronDown,
  ChevronRight,
  ChevronsDownUp,
  FileCode2,
  Folder,
  FolderOpen,
  FilePlus2,
  FolderPlus,
  RotateCcw,
  Search,
  X,
} from "lucide-react";
import { useEditor } from "../../../state/features/editor/index.js";
import { useWorkspace } from "../../../state/features/workspace/index.js";
import { useNotifications } from "../../../state/features/notifications/index.js";
import {
  collectAllDirs,
  filterFileTree,
  type FileEntry,
} from "../../../domain/features/workspace/index.js";
import { cn, ContextMenu, type ContextMenuEntry } from "../../primitives/index.js";
import { Button } from "../../primitives/button.js";

type PendingOp =
  | { readonly kind: "rename"; readonly path: string; readonly value: string }
  | {
      readonly kind: "create";
      readonly target: "file" | "folder";
      readonly parent: string;
      readonly value: string;
    }
  | null;

const INDENT_PX = 14;

export function FileTree() {
  const workspace = useWorkspace();
  const editor = useEditor();
  const notifications = useNotifications();
  const { tree: rawTree, currentPath } = workspace;
  const [pending, setPending] = useState<PendingOp>(null);
  const [filter, setFilter] = useState("");
  // Membership ⇒ "this folder path is collapsed". Empty set is
  // fully-expanded (matches how a fresh open behaves in most IDEs).
  const [collapsed, setCollapsed] = useState<ReadonlySet<string>>(
    () => new Set(),
  );

  const filtering = filter.trim().length > 0;
  const tree = useMemo(
    () => (rawTree ? filterFileTree(rawTree, filter) : null),
    [rawTree, filter],
  );

  const toggleCollapsed = useCallback((path: string) => {
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (next.has(path)) next.delete(path);
      else next.add(path);
      return next;
    });
  }, []);

  const rootName = currentPath ? currentPath.split("/").pop() || currentPath : "";

  if (!tree) {
    return (
      <div className="h-full flex flex-col">
        <PanelHeader title="Project" />
        <div
          style={{
            padding: "var(--gap-5)",
            fontSize: "var(--fs-12)",
            color: "var(--fg-2)",
          }}
        >
          Open a folder to see scripts.
        </div>
      </div>
    );
  }

  const onCreate = (parent: string, target: "file" | "folder") =>
    setPending({
      kind: "create",
      target,
      parent,
      value: target === "file" ? "new-script.yml" : "new-folder",
    });
  const onRename = (path: string) => {
    const name = path.split("/").pop() ?? "";
    setPending({ kind: "rename", path, value: name });
  };
  const onCancel = () => setPending(null);

  const onCommit = async () => {
    const op = pending;
    if (!op) return;
    setPending(null);
    try {
      if (op.kind === "create") {
        if (op.target === "file") {
          const newPath = await workspace.createScript(op.parent, op.value, "");
          await workspace.reloadTree();
          await editor.openFile(newPath);
        } else {
          await workspace.createFolder(op.parent, op.value);
          await workspace.reloadTree();
        }
      } else {
        const newPath = await workspace.renameScript(op.path, op.value);
        editor.renameTab(op.path, newPath);
        await workspace.reloadTree();
      }
    } catch (err) {
      notifications.show({
        kind: "error",
        title:
          op.kind === "create"
            ? op.target === "file"
              ? "Create file failed"
              : "Create folder failed"
            : "Rename failed",
        detail: err instanceof Error ? err.message : String(err),
      });
    }
  };

  return (
    <div className="pane h-full" style={{ borderRight: 0 }}>
      <PanelHeader
        title={rootName || "Project"}
        actions={
          <>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => onCreate(tree.rootPath, "file")}
              title="New File"
            >
              <FilePlus2 className="h-3 w-3" />
            </Button>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => onCreate(tree.rootPath, "folder")}
              title="New Folder"
            >
              <FolderPlus className="h-3 w-3" />
            </Button>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => void workspace.reloadTree()}
              title="Reload"
            >
              <RotateCcw className="h-3 w-3" />
            </Button>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => setCollapsed(collectAllDirs(tree))}
              title="Collapse all"
            >
              <ChevronsDownUp className="h-3 w-3" />
            </Button>
          </>
        }
      />
      <FilterInput
        value={filter}
        onChange={setFilter}
        onClear={() => setFilter("")}
      />
      <nav
        className="flex-1 min-h-0 overflow-y-auto"
        style={{ color: "var(--fg-1)", fontSize: "var(--fs-13)" }}
      >
        {pending?.kind === "create" && pending.parent === tree.rootPath ? (
          <InlineInput
            value={pending.value}
            onChange={(v) => setPending({ ...pending, value: v })}
            onCommit={onCommit}
            onCancel={onCancel}
            depth={0}
          />
        ) : null}
        {tree.entries.map((entry) => (
          <EntryNode
            key={entry.path}
            entry={entry}
            depth={0}
            pending={pending}
            collapsed={collapsed}
            forceExpanded={filtering}
            onToggleCollapsed={toggleCollapsed}
            onCreate={onCreate}
            onRename={onRename}
            onCancel={onCancel}
            onCommit={onCommit}
            setPending={setPending}
          />
        ))}
      </nav>
    </div>
  );
}

interface PanelHeaderProps {
  readonly title: string;
  readonly subtitle?: string;
  readonly actions?: React.ReactNode;
}

function PanelHeader({ title, subtitle, actions }: PanelHeaderProps) {
  return (
    <header className="pane-header">
      <span className="truncate" title={title}>
        {title}
      </span>
      {subtitle ? (
        <span
          className="truncate"
          title={subtitle}
          style={{
            fontSize: "var(--fs-11)",
            color: "var(--fg-3)",
            textTransform: "none",
            letterSpacing: 0,
            fontWeight: 400,
          }}
        >
          {subtitle}
        </span>
      ) : null}
      <div className="spacer" />
      {actions}
    </header>
  );
}

interface FilterInputProps {
  readonly value: string;
  readonly onChange: (value: string) => void;
  readonly onClear: () => void;
}

function FilterInput({ value, onChange, onClear }: FilterInputProps) {
  return (
    <div
      className="flex-none"
      style={{
        padding: "var(--gap-3) var(--gap-4)",
        borderBottom: "1px solid var(--line)",
      }}
    >
      <div
        className="flex items-center"
        style={{
          gap: "var(--gap-3)",
          padding: "var(--gap-2) var(--gap-3)",
          background: "var(--bg-3)",
          border: "1px solid var(--line)",
          borderRadius: "var(--r-2)",
        }}
      >
        <Search
          className="h-3 w-3 flex-none"
          style={{ color: "var(--fg-2)" }}
        />
        <input
          value={value}
          onChange={(e) => onChange(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Escape") {
              e.preventDefault();
              onClear();
            }
          }}
          placeholder="Filter…"
          className="flex-1 min-w-0 bg-transparent outline-none"
          style={{ color: "var(--fg-0)", fontSize: "var(--fs-12)" }}
        />
        {value.length > 0 ? (
          <button
            type="button"
            onClick={onClear}
            aria-label="Clear filter"
            className="flex-none opacity-60 hover:opacity-100"
            style={{ color: "var(--fg-2)" }}
          >
            <X className="h-3 w-3" />
          </button>
        ) : null}
      </div>
    </div>
  );
}

interface EntryNodeProps {
  readonly entry: FileEntry;
  readonly depth: number;
  readonly pending: PendingOp;
  readonly collapsed: ReadonlySet<string>;
  readonly forceExpanded: boolean;
  readonly onToggleCollapsed: (path: string) => void;
  readonly onCreate: (parent: string, target: "file" | "folder") => void;
  readonly onRename: (path: string) => void;
  readonly onCancel: () => void;
  readonly onCommit: () => Promise<void>;
  readonly setPending: (op: PendingOp) => void;
}

function EntryNode({
  entry,
  depth,
  pending,
  collapsed,
  forceExpanded,
  onToggleCollapsed,
  onCreate,
  onRename,
  onCancel,
  onCommit,
  setPending,
}: EntryNodeProps) {
  const { openFile, activePath, tabs, closeFile } = useEditor();
  const workspace = useWorkspace();
  const notifications = useNotifications();
  const isRenaming = pending?.kind === "rename" && pending.path === entry.path;

  const deleteEntry = async () => {
    if (entry.type === "directory") {
      // Recursive removal wipes every file inside — gate it on an
      // explicit confirmation so a mis-click on a deep folder
      // doesn't silently delete a subtree.
      const confirmed =
        typeof window === "undefined"
          ? true
          : window.confirm(
              `Delete folder "${entry.name}" and everything inside it? This cannot be undone.`,
            );
      if (!confirmed) return;
    }
    try {
      await workspace.deleteScript(entry.path);
      if (entry.type === "directory") {
        // Close every open tab whose file lived under the deleted
        // folder, not just the folder path itself.
        const prefix = entry.path.replace(/\/$/, "") + "/";
        for (const tab of tabs) {
          if (tab.path === entry.path || tab.path.startsWith(prefix)) {
            closeFile(tab.path);
          }
        }
      } else {
        closeFile(entry.path);
      }
      await workspace.reloadTree();
    } catch (err) {
      notifications.show({
        kind: "error",
        title: "Delete failed",
        detail: err instanceof Error ? err.message : String(err),
      });
    }
  };

  const items: readonly ContextMenuEntry[] =
    entry.type === "directory"
      ? [
          {
            id: "new-file",
            label: "New File",
            onSelect: () => onCreate(entry.path, "file"),
          },
          {
            id: "new-folder",
            label: "New Folder",
            onSelect: () => onCreate(entry.path, "folder"),
          },
          "separator",
          {
            id: "rename",
            label: "Rename…",
            onSelect: () => onRename(entry.path),
          },
          "separator",
          {
            id: "delete",
            label: "Delete",
            danger: true,
            onSelect: () => void deleteEntry(),
          },
        ]
      : [
          {
            id: "rename",
            label: "Rename…",
            shortcut: "F2",
            onSelect: () => onRename(entry.path),
          },
          "separator",
          {
            id: "delete",
            label: "Delete",
            danger: true,
            onSelect: () => void deleteEntry(),
          },
        ];

  if (entry.type === "directory") {
    const isCollapsed = !forceExpanded && collapsed.has(entry.path);
    return (
      <div>
        <ContextMenu items={items}>
          {({ onContextMenu }) =>
            isRenaming ? (
              <InlineInput
                value={pending!.value}
                onChange={(v) => setPending({ ...pending!, value: v })}
                onCommit={onCommit}
                onCancel={onCancel}
                depth={depth}
              />
            ) : (
              <button
                type="button"
                onContextMenu={onContextMenu}
                onClick={() => onToggleCollapsed(entry.path)}
                className="tree-row w-full text-left"
                style={{ paddingLeft: 10 + depth * INDENT_PX }}
              >
                <span className="chev">
                  {isCollapsed ? (
                    <ChevronRight />
                  ) : (
                    <ChevronDown />
                  )}
                </span>
                <span className="ficon">
                  {isCollapsed ? <Folder /> : <FolderOpen />}
                </span>
                <span className="fname">{entry.name}</span>
              </button>
            )
          }
        </ContextMenu>
        {!isCollapsed ? (
          <>
            {pending?.kind === "create" && pending.parent === entry.path ? (
              <InlineInput
                value={pending.value}
                onChange={(v) => setPending({ ...pending, value: v })}
                onCommit={onCommit}
                onCancel={onCancel}
                depth={depth + 1}
              />
            ) : null}
            <div>
              {entry.children?.map((child) => (
                <EntryNode
                  key={child.path}
                  entry={child}
                  depth={depth + 1}
                  pending={pending}
                  collapsed={collapsed}
                  forceExpanded={forceExpanded}
                  onToggleCollapsed={onToggleCollapsed}
                  onCreate={onCreate}
                  onRename={onRename}
                  onCancel={onCancel}
                  onCommit={onCommit}
                  setPending={setPending}
                />
              ))}
            </div>
          </>
        ) : null}
      </div>
    );
  }

  const active = entry.path === activePath;
  const dirty = tabs.some((t) => t.path === entry.path && t.dirty);
  return (
    <ContextMenu items={items}>
      {({ onContextMenu }) =>
        isRenaming ? (
          <InlineInput
            value={pending!.value}
            onChange={(v) => setPending({ ...pending!, value: v })}
            onCommit={onCommit}
            onCancel={onCancel}
            depth={depth}
          />
        ) : (
          <button
            type="button"
            onContextMenu={onContextMenu}
            onClick={() => openFile(entry.path)}
            className={cn(
              "tree-row file w-full text-left",
              active && "selected",
            )}
            style={{ paddingLeft: 10 + depth * INDENT_PX }}
          >
            <span className="chev" />
            <span className="ficon">
              <FileCode2 />
            </span>
            <span className={cn("fname", dirty && "dirty")}>{entry.name}</span>
          </button>
        )
      }
    </ContextMenu>
  );
}

interface InlineInputProps {
  readonly value: string;
  readonly onChange: (value: string) => void;
  readonly onCommit: () => void | Promise<void>;
  readonly onCancel: () => void;
  readonly depth: number;
}

function InlineInput({
  value,
  onChange,
  onCommit,
  onCancel,
  depth,
}: InlineInputProps) {
  return (
    <div
      className="px-2 py-0.5"
      style={{ paddingLeft: 8 + depth * INDENT_PX }}
    >
      <input
        autoFocus
        value={value}
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.preventDefault();
            void onCommit();
          } else if (e.key === "Escape") {
            e.preventDefault();
            onCancel();
          }
        }}
        onBlur={() => void onCommit()}
        className="w-full outline-none"
        style={{
          padding: "var(--gap-1) var(--gap-3)",
          borderRadius: "var(--r-2)",
          fontSize: "var(--fs-12)",
          background: "var(--bg-3)",
          color: "var(--fg-0)",
          border: "1px solid var(--accent-border)",
        }}
      />
    </div>
  );
}
