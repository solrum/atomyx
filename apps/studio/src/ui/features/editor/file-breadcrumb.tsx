import { Folder } from "lucide-react";
import { useEditor } from "../../../state/features/editor/index.js";
import { useWorkspace } from "../../../state/features/workspace/index.js";

/**
 * Path breadcrumb above the editor. Shows the workspace name +
 * each folder segment leading to the active file. Segments are
 * intentionally read-only for now — clicking does not change
 * state, but keeps the user oriented in deep workspaces.
 */
export function FileBreadcrumb() {
  const { currentPath } = useWorkspace();
  const { tabs, activePath } = useEditor();
  const active = tabs.find((t) => t.path === activePath);
  if (!currentPath || !active) return null;

  const segments = computeSegments(currentPath, active.path);
  if (segments.length === 0) return null;

  return (
    <nav className="breadcrumbs">
      <Folder style={{ width: "11px", height: "11px" }} />
      {segments.map((seg, i) => (
        <span key={i} className="flex items-center">
          {i > 0 ? <span className="sep">›</span> : null}
          <span className={i === segments.length - 1 ? "cur" : undefined}>
            {seg}
          </span>
        </span>
      ))}
    </nav>
  );
}

function computeSegments(workspacePath: string, filePath: string): string[] {
  const workspaceName = basename(workspacePath);
  if (!filePath.startsWith(workspacePath)) return [workspaceName, filePath];
  const rel = filePath.slice(workspacePath.length).replace(/^\/+/, "");
  return [workspaceName, ...rel.split("/").filter(Boolean)];
}

function basename(path: string): string {
  const trimmed = path.replace(/\/+$/, "");
  const idx = trimmed.lastIndexOf("/");
  return idx === -1 ? trimmed : trimmed.slice(idx + 1);
}
