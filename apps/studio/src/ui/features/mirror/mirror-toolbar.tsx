import { X } from "lucide-react";

import { useIosAgentStatus } from "../../../state/features/ios-agent/index.js";
import type { IosAgentStatus } from "../../../state/features/ios-agent/index.js";
import {
  agentStateColor,
  agentStateLabel,
  agentStateTooltip,
} from "../../../domain/features/ios-agent/index.js";
import { getFeature } from "../../../state/core/registry.js";
import type { MirrorApi, MirrorSessionStatus } from "../../../state/features/mirror/index.js";
import { MIRROR_KEY } from "../../../state/features/mirror/index.js";
import { MirrorTextInsert } from "./mirror-text-insert.js";

export function MirrorToolbar({
  session,
}: {
  readonly session: MirrorSessionStatus;
}) {
  const isIos =
    session.target.kind === "ios-simulator" ||
    session.target.kind === "ios-device";
  const iosStatus = useIosAgentStatus(isIos ? session.target.id : null);

  const onStopMirror = async () => {
    try {
      await getFeature<MirrorApi>(MIRROR_KEY).stop(session.id);
    } catch (err) {
      console.error("[mirror-toolbar] stop failed", err);
    }
  };

  return (
    <div
      className="flex items-center gap-2 border-b px-2 py-1 text-xs"
      style={{ borderColor: "var(--line)" }}
    >
      {isIos ? <IosAgentChip status={iosStatus} /> : null}
      <div className="flex-1" />
      <MirrorTextInsert session={session} />
      <button
        type="button"
        onClick={onStopMirror}
        className="inline-flex items-center gap-1 rounded px-2 py-0.5 hover:bg-[color:var(--bg-hover)]"
        title="Stop mirror"
      >
        <X className="h-3 w-3" />
        <span>Stop</span>
      </button>
    </div>
  );
}

function IosAgentChip({ status }: { readonly status: IosAgentStatus | null }) {
  const state = status?.state ?? "idle";
  const color = agentStateColor(state);
  const label = agentStateLabel(state);
  const title = agentStateTooltip(status);
  return (
    <span
      className="inline-flex items-center gap-1"
      style={{ color }}
      title={title}
    >
      <span
        className="inline-block h-2 w-2 rounded-full"
        style={{ backgroundColor: color }}
      />
      <span>{label}</span>
    </span>
  );
}
