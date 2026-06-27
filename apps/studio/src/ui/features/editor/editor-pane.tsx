import { useEditor } from "../../../state/features/editor/index.js";
import { useLayout } from "../../../state/features/layout/index.js";
import { EditorTabs } from "./editor-tabs.js";
import { ScriptEditor } from "./editor-script-editor.js";
import { FileBreadcrumb } from "./editor-file-breadcrumb.js";
import { GlobalRunBar } from "../runs/index.js";
import { StructureView } from "../tool-windows/index.js";
import { jumpActiveEditorTo } from "./editor-monaco-active.js";
import { getFeature } from "../../../state/core/registry.js";
import {
  type EditorApi,
  EDITOR_KEY,
  type EditorGroup,
} from "../../../state/features/editor/index.js";

export function EditorPane() {
  const { groups, activeGroupId } = useEditor();
  const { structureVisible } = useLayout();

  const canSplit = groups.length < 2;

  return (
    <div className="flex h-full">
      {groups.map((group, idx) => (
        <GroupPane
          key={group.id}
          group={group}
          isActive={group.id === activeGroupId}
          canSplit={canSplit && idx === groups.length - 1}
          canClose={groups.length > 1}
          showBreadcrumb={idx === 0}
        />
      ))}
      {structureVisible ? (
        <aside
          className="w-56 border-l overflow-y-auto"
          style={{
            background: "var(--bg-2)",
            borderColor: "var(--line)",
          }}
        >
          <div
            className="px-3 py-2 text-[11px] uppercase tracking-wider border-b"
            style={{
              color: "var(--fg-2)",
              borderColor: "var(--line)",
            }}
          >
            Structure
          </div>
          <StructureView onJumpToLine={jumpActiveEditorTo} />
        </aside>
      ) : null}
    </div>
  );
}

interface GroupPaneProps {
  readonly group: EditorGroup;
  readonly isActive: boolean;
  readonly canSplit: boolean;
  readonly canClose: boolean;
  readonly showBreadcrumb: boolean;
}

function GroupPane({
  group,
  isActive,
  canSplit,
  canClose,
  showBreadcrumb,
}: GroupPaneProps) {
  const active = group.tabs.find((t) => t.path === group.activePath);
  const updateContent = (path: string, next: string) => {
    getFeature<EditorApi>(EDITOR_KEY).focusGroup(group.id);
    getFeature<EditorApi>(EDITOR_KEY).updateContent(path, next);
  };

  return (
    <div
      className="center-col flex-1 min-w-0 min-h-0 h-full"
      style={{
        gridTemplateRows: showBreadcrumb
          ? "auto auto auto 1fr"
          : "auto auto 1fr",
        borderLeft: isActive ? "1px solid var(--accent)" : undefined,
      }}
    >
      <EditorTabs group={group} canSplit={canSplit} canClose={canClose} />
      {showBreadcrumb ? <FileBreadcrumb /> : null}
      {showBreadcrumb && active ? <GlobalRunBar /> : null}
      <div className="editor-host">
        {active ? (
          <ScriptEditor
            key={`${group.id}-${active.path}`}
            value={active.content}
            onChange={(next) => updateContent(active.path, next)}
            groupId={group.id}
          />
        ) : (
          <div className="h-full flex flex-col items-center justify-center">
            <div
              style={{
                width: 48,
                height: 48,
                borderRadius: "50%",
                background: "var(--accent-bg)",
                display: "grid",
                placeItems: "center",
                marginBottom: 12,
              }}
            >
              <span
                style={{
                  fontSize: 22,
                  color: "var(--accent)",
                  lineHeight: 1,
                }}
              >
                ⌘
              </span>
            </div>
            <div
              style={{
                fontSize: "var(--fs-14)",
                color: "var(--fg-1)",
                marginBottom: 4,
              }}
            >
              No file open
            </div>
            <div
              className="flex items-center"
              style={{ gap: 8, fontSize: 11, color: "var(--fg-2)" }}
            >
              <span className="kbd">⌘P</span> to find files
              <span className="kbd">⇧⇧</span> to find everywhere
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
