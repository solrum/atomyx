import { useMirror } from "../../../state/features/mirror/index.js";
import type { MirrorApi } from "../../../state/features/mirror/index.js";
import { MIRROR_KEY } from "../../../state/features/mirror/index.js";
import {
  deviceToMirrorTarget,
  useDevices,
} from "../../../state/features/devices/index.js";
import type { IosAgentApi } from "../../../state/features/ios-agent/index.js";
import { IOS_AGENT_KEY } from "../../../state/features/ios-agent/index.js";
import type { NotificationsApi } from "../../../state/features/notifications/index.js";
import { NOTIFICATIONS_KEY } from "../../../state/features/notifications/index.js";
import { getFeature } from "../../../state/core/registry.js";
import { MirrorPane } from "./mirror-pane.js";

interface MirrorFrameProps {
  /** When true, paint the iPhone-style border + rounded chrome. */
  readonly chrome?: boolean;
  /** Hide MirrorPane's inline toolbar (stop / record / screenshot). */
  readonly hideToolbar?: boolean;
}

/**
 * Reusable embedded mirror surface. When a session exists, renders
 * the live `MirrorPane`; otherwise shows the device label + a
 * "Start mirror" button that starts a session for the currently
 * selected device. Used by both the floating window and the docked
 * slot — keeps the start-session flow identical in both placements.
 */
export function MirrorFrame({
  chrome = false,
  hideToolbar = false,
}: MirrorFrameProps = {}) {
  const mirrorSnap = useMirror();
  const { devices, selectedId } = useDevices();
  const activeSession = Object.values(mirrorSnap.sessions)[0] ?? null;
  const hasSession = activeSession !== null;
  const activeDevice = hasSession
    ? (devices.find((d) => d.id === activeSession.target.id) ?? null)
    : (devices.find((d) => d.id === selectedId) ?? devices[0] ?? null);

  async function startSession() {
    if (!activeDevice) return;
    try {
      const target = deviceToMirrorTarget(activeDevice);
      if (activeDevice.platform === "ios") {
        const agentKind =
          activeDevice.kind === "simulator" ? "simulator" : "device";
        void getFeature<IosAgentApi>(IOS_AGENT_KEY)
          .ensure(activeDevice.id, agentKind)
          .catch((err) => {
            console.error("[mirror-frame] iosAgent.ensure failed", err);
          });
      }
      await getFeature<MirrorApi>(MIRROR_KEY).startForTarget(target, {
        maxSize: 1080,
        bitrate: 8_000_000,
      });
    } catch (err) {
      getFeature<NotificationsApi>(NOTIFICATIONS_KEY).show({
        kind: "error",
        title: "Mirror failed to start",
        detail: err instanceof Error ? err.message : String(err),
        ttlMs: 10000,
        pinned: true,
      });
    }
  }

  const sessionAspect =
    activeSession &&
    activeSession.videoWidth > 0 &&
    activeSession.videoHeight > 0
      ? activeSession.videoWidth / activeSession.videoHeight
      : 9 / 19.5;

  if (chrome) {
    return (
      <div
        style={{
          aspectRatio: sessionAspect,
          height: "100%",
          maxWidth: "100%",
          background: "#1a1d22",
          border: "3px solid var(--bg-3)",
          borderRadius: "32px",
          position: "relative",
          overflow: "hidden",
          boxShadow: "var(--shadow-md)",
        }}
      >
        <div style={{ position: "absolute", inset: 0 }}>
          {hasSession ? (
            <MirrorPane hideToolbar={hideToolbar} />
          ) : (
            <Empty activeDevice={activeDevice} startSession={startSession} />
          )}
        </div>
      </div>
    );
  }
  return (
    <div
      style={{
        flex: 1,
        minWidth: 0,
        minHeight: 0,
        display: "flex",
        flexDirection: "column",
        overflow: "hidden",
      }}
    >
      {hasSession ? (
            <MirrorPane hideToolbar={hideToolbar} />
          ) : (
            <Empty activeDevice={activeDevice} startSession={startSession} />
          )}
    </div>
  );
}

interface EmptyProps {
  readonly activeDevice: { readonly name: string; readonly platform: string } | null;
  readonly startSession: () => Promise<void>;
}

function Empty({ activeDevice, startSession }: EmptyProps) {
  return (
    <div
      style={{
        flex: 1,
        minHeight: 0,
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "center",
        fontSize: "var(--fs-11)",
        color: "var(--fg-3)",
        textAlign: "center",
        padding: "var(--gap-4)",
        gap: "var(--gap-3)",
      }}
    >
      <span>
        {activeDevice
          ? `${activeDevice.name} · ${activeDevice.platform}`
          : "No device selected"}
      </span>
      <button
        type="button"
        onClick={() => void startSession()}
        disabled={!activeDevice}
        className="atomyx-btn atomyx-btn-primary"
        style={{
          height: "26px",
          padding: "0 var(--gap-4)",
          fontSize: "var(--fs-12)",
          borderRadius: "var(--r-2)",
        }}
      >
        Start mirror
      </button>
    </div>
  );
}
