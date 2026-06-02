import { useWorkspace } from "../../../state/features/workspace/index.js";
import { useProjects } from "../../../state/features/projects/index.js";
import { Atom, FolderOpen, FilePlus2, Copy } from "lucide-react";

export interface WelcomeProps {
  readonly onNewTest: () => void;
}

export function Welcome({ onNewTest }: WelcomeProps) {
  const { pickAndOpen, openFolder } = useWorkspace();
  const { items: recents } = useProjects();
  return (
    <div className="empty">
      <div className="card">
        <div className="logo-mark">
          <Atom style={{ color: "var(--accent)" }} />
        </div>
        <h1>Atomyx Studio</h1>
        <p>
          Author, validate, and run YAML test scripts on real devices.
        </p>
        <div className="cta-row">
          <button type="button" className="btn primary" onClick={pickAndOpen}>
            <FolderOpen style={iconSm} /> Open folder
          </button>
          <button type="button" className="btn" onClick={onNewTest}>
            <FilePlus2 style={iconSm} /> New test
          </button>
          <button
            type="button"
            className="btn ghost"
            onClick={pickAndOpen}
            title="Clone a sample workspace"
          >
            <Copy style={iconSm} /> Clone sample
          </button>
        </div>
        {recents.length > 0 ? (
          <div className="recent">
            <div className="recent-title">Recent</div>
            {recents.slice(0, 5).map((p) => (
              <button
                key={p.path}
                type="button"
                onClick={() => void openFolder(p.path)}
                className="recent-row text-left w-full"
              >
                <div className="min-w-0">
                  <div className="p truncate">{p.displayName}</div>
                  <div className="path truncate">{p.path}</div>
                </div>
                <div className="meta">{relTime(p.lastOpenedAt)}</div>
              </button>
            ))}
          </div>
        ) : null}
        <div
          className="flex items-center justify-center"
          style={{
            gap: "12px",
            marginTop: "20px",
            paddingTop: "16px",
            borderTop: "1px solid var(--line-soft)",
            fontSize: "11px",
            color: "var(--fg-3)",
          }}
        >
          <span><span className="kbd">⌘O</span> open</span>
          <span><span className="kbd">⌘N</span> new</span>
          <span><span className="kbd">⌘,</span> settings</span>
        </div>
      </div>
    </div>
  );
}

const iconSm = { width: "14px", height: "14px" } as const;

function relTime(ts: number): string {
  const diff = Math.max(0, Date.now() - ts);
  const m = Math.floor(diff / 60_000);
  if (m < 1) return "just now";
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  const d = Math.floor(h / 24);
  return `${d}d ago`;
}
