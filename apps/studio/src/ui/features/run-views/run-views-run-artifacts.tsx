import { useMemo } from "react";
import type { LiveRun } from "../../../state/features/runs/index.js";

interface ArtifactsViewProps {
  readonly live: LiveRun | null;
}

export function RunArtifacts({ live }: ArtifactsViewProps) {
  const screenshots = useMemo(() => {
    if (!live) return [];
    return live.events.filter((e) => e.type === "screenshot");
  }, [live]);

  if (!live) {
    return (
      <div className="artifacts">
        <div className="hdr">Artifacts</div>
        <div style={{ color: "var(--fg-2)", fontSize: 11 }}>
          Run will produce screenshots, HAR, and UI-tree artifacts.
        </div>
      </div>
    );
  }
  if (screenshots.length === 0) {
    return (
      <div className="artifacts">
        <div className="hdr">Artifacts</div>
        <div style={{ color: "var(--fg-2)", fontSize: 11 }}>
          No artifacts captured yet.
        </div>
      </div>
    );
  }
  return (
    <div className="artifacts">
      <div className="hdr">Artifacts · {screenshots.length}</div>
      <div className="artifact-grid">
        {screenshots.map((e, i) => {
          if (e.type !== "screenshot") return null;
          const name = e.label ?? `step-${e.stepIndex + 1}.png`;
          return (
            <div key={i} className="artifact" title={name}>
              <div className="thumb">
                <span className="badge">{e.stepIndex + 1}</span>
              </div>
              <div className="fname">{name}</div>
            </div>
          );
        })}
      </div>
    </div>
  );
}
