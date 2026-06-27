import { useCallback, useEffect, useRef, useState } from "react";
import {
  Smartphone,
  Crosshair,
  Maximize2,
  Move,
  PanelRight,
  X,
  RotateCcw,
  Square,
} from "lucide-react";
import {
  useMirrorWindow,
  type MirrorDock,
  type MirrorWindowMode,
} from "../../../state/features/mirror-window/index.js";
import { useRuns, type LiveRun } from "../../../state/features/runs/index.js";
import { useDevices } from "../../../state/features/devices/index.js";
import { useRuntimeStatus } from "../../../state/features/runtime-status/index.js";
import { useMirror } from "../../../state/features/mirror/index.js";
import type { MirrorApi } from "../../../state/features/mirror/index.js";
import { MIRROR_KEY } from "../../../state/features/mirror/index.js";
import type { NotificationsApi } from "../../../state/features/notifications/index.js";
import { NOTIFICATIONS_KEY } from "../../../state/features/notifications/index.js";
import { getFeature } from "../../../state/core/registry.js";
import { MirrorInspectorPane } from "../mirror/index.js";

/**
 * Floating device-mirror tool window. Mirrors the bundle design's
 * three-mode layout:
 *
 *   - compact    340×600 float / 360 docked — phone frame only
 *   - inspector  640×620 float / 640 docked — phone + inspector
 *   - full       inset:0 — phone + event stream + scrubber
 *
 * Free or right-docked. Free position is dragged from the titlebar
 * with window-level pointermove listeners that detach on release;
 * docked snaps to the right of the workbench between the title and
 * status bars.
 */
export function DeviceMirrorWindow() {
  const win = useMirrorWindow();
  const containerRef = useRef<HTMLDivElement>(null);

  const startDrag = useCallback(
    (clientX: number, clientY: number) => {
      if (win.dock === "right" || win.mode === "full") return;
      const startX = clientX;
      const startY = clientY;
      const startPos = win.position;

      function onMove(ev: globalThis.PointerEvent) {
        win.setPosition({
          x: Math.max(80, Math.min(window.innerWidth - 200, startPos.x + ev.clientX - startX)),
          y: Math.max(40, Math.min(window.innerHeight - 140, startPos.y + ev.clientY - startY)),
        });
      }
      function onUp() {
        window.removeEventListener("pointermove", onMove);
        window.removeEventListener("pointerup", onUp);
        window.removeEventListener("pointercancel", onUp);
      }
      window.addEventListener("pointermove", onMove);
      window.addEventListener("pointerup", onUp);
      window.addEventListener("pointercancel", onUp);
    },
    [win],
  );

  if (!win.isOpen) return null;

  const isFull = win.mode === "full";
  const isDocked = win.dock === "right" && !isFull;
  // Docked-right is owned by the in-grid MirrorSlot now; only render
  // this overlay for full-screen and floating modes.
  if (isDocked) return null;
  const containerStyle: React.CSSProperties = isFull
    ? {
        position: "fixed",
        top: "30px",
        right: 0,
        bottom: "22px",
        left: "44px",
        zIndex: 30,
      }
    : isDocked
      ? {
          position: "fixed",
          right: 0,
          top: "30px",
          bottom: "22px",
          width: win.mode === "inspector" ? "640px" : "360px",
          zIndex: 20,
        }
      : {
          position: "fixed",
          left: `${win.position.x}px`,
          top: `${win.position.y}px`,
          width: win.mode === "inspector" ? "640px" : "340px",
          height: win.mode === "inspector" ? "620px" : "600px",
          zIndex: 20,
        };

  return (
    <div
      ref={containerRef}
      className="flex flex-col"
      style={{
        ...containerStyle,
        background: "var(--bg-1)",
        border: "1px solid var(--line)",
        borderRadius: isFull ? 0 : "var(--r-4)",
        boxShadow: isFull ? "none" : "var(--shadow-lg)",
        overflow: "hidden",
        color: "var(--fg-1)",
      }}
    >
      <Titlebar
        mode={win.mode}
        dock={win.dock}
        onStartDrag={startDrag}
        onSetMode={win.setMode}
        onSetDock={win.setDock}
        onClose={win.close}
      />
      <div className="flex-1 min-h-0 flex flex-col">
        <Stage mode={win.mode} />
        {isFull ? <EventStream /> : null}
        <Scrubber />
      </div>
    </div>
  );
}

function Titlebar({
  mode,
  dock,
  onStartDrag,
  onSetMode,
  onSetDock,
  onClose,
}: {
  readonly mode: MirrorWindowMode;
  readonly dock: MirrorDock;
  readonly onStartDrag: (clientX: number, clientY: number) => void;
  readonly onSetMode: (mode: MirrorWindowMode) => void;
  readonly onSetDock: (dock: MirrorDock) => void;
  readonly onClose: () => void;
}) {
  const { devices, selectedId } = useDevices();
  const mirrorSnap = useMirror();
  const runtime = useRuntimeStatus();
  const activeSession = Object.values(mirrorSnap.sessions)[0] ?? null;
  // Prefer the device the live session is bound to so the label
  // tracks the streaming source rather than a stale picker selection.
  const activeDevice = activeSession
    ? (devices.find((d) => d.id === activeSession.target.id) ?? null)
    : (devices.find((d) => d.id === selectedId) ?? devices[0] ?? null);
  const connected = runtime.status === "connected";
  const isFull = mode === "full";
  const dockable = !isFull;
  const draggable = dock === "free" && !isFull;

  return (
    <div
      onPointerDown={(e) => {
        if (e.button !== 0 || !draggable) return;
        onStartDrag(e.clientX, e.clientY);
      }}
      className="dm-header select-none"
      style={{
        height: "30px",
        cursor: draggable ? "grab" : "default",
      }}
    >
      <Smartphone style={{ width: "12px", height: "12px" }} />
      <span style={{ color: "var(--fg-0)", fontWeight: 600 }}>
        Device Mirror
      </span>
      <span style={{ color: "var(--fg-3)" }}>·</span>
      <span>
        {activeDevice ? `${activeDevice.name} · ${activeDevice.platform}` : "no device"}
      </span>
      <span
        className="flex items-center"
        style={{
          gap: "var(--gap-1)",
          marginLeft: "var(--gap-3)",
          color: connected ? "var(--ok)" : "var(--fg-3)",
          textTransform: "none",
          letterSpacing: 0,
        }}
      >
        <span
          aria-hidden
          style={{
            width: "6px",
            height: "6px",
            borderRadius: "50%",
            background: connected ? "var(--ok)" : "var(--fg-3)",
          }}
        />
        {connected ? "connected" : "offline"}
      </span>

      <span className="spacer" />

      <ModeGroup>
        <ModeBtn
          active={mode === "compact"}
          onClick={() => onSetMode("compact")}
          label="Compact — phone only"
        >
          <Smartphone style={iconXs} />
        </ModeBtn>
        <ModeBtn
          active={mode === "inspector"}
          onClick={() => onSetMode("inspector")}
          label="Inspector — phone + DOM"
        >
          <Crosshair style={iconXs} />
        </ModeBtn>
        <ModeBtn
          active={mode === "full"}
          onClick={() => onSetMode("full")}
          label="Full screen"
        >
          <Maximize2 style={iconXs} />
        </ModeBtn>
      </ModeGroup>
      {dockable ? (
        <ModeGroup>
          <ModeBtn
            active={dock === "free"}
            onClick={() => onSetDock("free")}
            label="Float"
          >
            <Move style={iconXs} />
          </ModeBtn>
          <ModeBtn
            active={dock === "right"}
            onClick={() => onSetDock("right")}
            label="Dock right"
          >
            <PanelRight style={iconXs} />
          </ModeBtn>
        </ModeGroup>
      ) : null}
      {activeSession ? (
        <ModeBtn
          onClick={() => {
            void getFeature<MirrorApi>(MIRROR_KEY)
              .stop(activeSession.id)
              .catch((err) => {
                getFeature<NotificationsApi>(NOTIFICATIONS_KEY).show({
                  kind: "error",
                  title: "Stop mirror failed",
                  detail: err instanceof Error ? err.message : String(err),
                  ttlMs: 8000,
                });
              });
          }}
          label="Stop mirror"
        >
          <Square style={iconXs} />
        </ModeBtn>
      ) : null}
      <ModeBtn onClick={onClose} label="Close">
        <X style={iconXs} />
      </ModeBtn>
    </div>
  );
}

const iconXs = { width: "11px", height: "11px" } as const;

function ModeGroup({ children }: { readonly children: React.ReactNode }) {
  return (
    <div
      className="flex items-center"
      style={{
        gap: "1px",
        padding: "1px",
        background: "var(--bg-3)",
        border: "1px solid var(--line)",
        borderRadius: "var(--r-2)",
      }}
    >
      {children}
    </div>
  );
}

function ModeBtn({
  active,
  onClick,
  label,
  children,
}: {
  readonly active?: boolean;
  readonly onClick: () => void;
  readonly label: string;
  readonly children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={(e) => {
        e.stopPropagation();
        onClick();
      }}
      onPointerDown={(e) => e.stopPropagation()}
      aria-label={label}
      title={label}
      className="flex items-center justify-center"
      style={{
        width: "20px",
        height: "20px",
        color: active ? "var(--fg-0)" : "var(--fg-2)",
        background: active ? "var(--bg-hover)" : "transparent",
        border: "none",
        borderRadius: "var(--r-1)",
      }}
    >
      {children}
    </button>
  );
}

function Stage({ mode }: { readonly mode: MirrorWindowMode }) {
  const showInspector = mode === "inspector" || mode === "full";
  return (
    <div
      className="flex-1 min-h-0 flex"
      style={{ background: "var(--bg-0)", padding: "var(--gap-3)" }}
    >
      <MirrorInspectorPane showInspector={showInspector} chrome />
    </div>
  );
}

function EventStream() {
  const { live } = useRuns();
  const recent = live ? live.events.slice(-12) : [];
  return (
    <div
      className="flex-none flex flex-col"
      style={{
        height: "180px",
        borderTop: "1px solid var(--line)",
        background: "var(--bg-2)",
      }}
    >
      <div
        className="flex items-center flex-none uppercase tracking-wider"
        style={{
          height: "22px",
          padding: "0 var(--gap-4)",
          gap: "var(--gap-2)",
          fontSize: "var(--fs-11)",
          color: "var(--fg-2)",
          borderBottom: "1px solid var(--line)",
        }}
      >
        <span>Event stream</span>
        <span style={{ flex: 1 }} />
        <span style={{ color: "var(--fg-3)" }}>
          {live?.result === "running" ? "Live" : "Paused"}
        </span>
      </div>
      <div
        className="flex-1 min-h-0 overflow-auto"
        style={{
          padding: "var(--gap-2) var(--gap-4)",
          fontSize: "var(--fs-11)",
          fontFamily: "var(--font-mono)",
          color: "var(--fg-2)",
        }}
      >
        {!live ? (
          <span style={{ color: "var(--fg-3)" }}>No active run.</span>
        ) : (
          recent.map((e, i) => (
            <div key={i}>
              {e.type === "stepStarted" && (
                <>
                  <span style={{ color: "var(--fg-3)" }}>#{e.stepIndex + 1}</span>{" "}
                  {e.command}
                </>
              )}
              {e.type === "stepCompleted" && (
                <span style={{ color: e.ok ? "var(--ok)" : "var(--err)" }}>
                  #{e.stepIndex + 1} {e.ok ? "pass" : "fail"} ({e.durationMs}ms)
                </span>
              )}
              {e.type === "consoleLog" && (
                <span
                  style={{
                    color:
                      e.level === "error"
                        ? "var(--err)"
                        : e.level === "warn"
                          ? "var(--warn)"
                          : "var(--fg-2)",
                  }}
                >
                  {e.line}
                </span>
              )}
            </div>
          ))
        )}
      </div>
    </div>
  );
}

function Scrubber() {
  const { live } = useRuns();
  const win = useMirrorWindow();
  const { totalSteps, completedSteps, failedAt } = stepStats(live);
  const activeStep = win.scrubStep ?? completedSteps;

  if (totalSteps === 0) {
    return (
      <div
        className="flex items-center flex-none"
        style={{
          height: "26px",
          padding: "0 var(--gap-4)",
          gap: "var(--gap-3)",
          borderTop: "1px solid var(--line)",
          background: "var(--chrome-bg)",
          fontSize: "var(--fs-11)",
          color: "var(--fg-3)",
        }}
      >
        idle
      </div>
    );
  }

  return (
    <div
      className="flex items-center flex-none"
      style={{
        height: "26px",
        padding: "0 var(--gap-4)",
        gap: "var(--gap-3)",
        borderTop: "1px solid var(--line)",
        background: "var(--chrome-bg)",
      }}
    >
      <button
        type="button"
        onClick={() => win.setScrubStep(null)}
        title="Follow live"
        className="flex items-center justify-center"
        style={{
          width: "18px",
          height: "18px",
          background: "transparent",
          color: win.scrubStep === null ? "var(--fg-0)" : "var(--fg-2)",
          borderRadius: "var(--r-1)",
        }}
      >
        <RotateCcw style={{ width: "10px", height: "10px" }} />
      </button>
      <div className="flex-1 flex items-center" style={{ gap: "2px" }}>
        {Array.from({ length: totalSteps }).map((_, i) => {
          const isPassed = i < completedSteps && i !== failedAt;
          const isFailed = i === failedAt;
          const isCur = i === activeStep;
          const color = isFailed
            ? "var(--err)"
            : isPassed
              ? "var(--ok)"
              : "var(--bg-3)";
          return (
            <button
              key={i}
              type="button"
              onClick={() => win.setScrubStep(i)}
              title={`Step ${i + 1}`}
              aria-label={`Step ${i + 1}`}
              className="flex-1"
              style={{
                height: isCur ? "10px" : "6px",
                minWidth: "4px",
                background: color,
                borderRadius: "1px",
                border: isCur ? "1px solid var(--accent)" : "none",
                transition: "height 0.1s ease",
              }}
            />
          );
        })}
      </div>
      <span
        className="tabular-nums"
        style={{ fontSize: "var(--fs-11)", color: "var(--fg-3)" }}
      >
        {win.scrubStep === null
          ? `live · ${completedSteps}/${totalSteps}`
          : `step ${activeStep + 1}/${totalSteps}`}
      </span>
    </div>
  );
}

function stepStats(live: LiveRun | null): {
  readonly totalSteps: number;
  readonly completedSteps: number;
  readonly failedAt: number;
} {
  if (!live) return { totalSteps: 0, completedSteps: 0, failedAt: -1 };
  const started = live.events.filter((e) => e.type === "stepStarted").length;
  const completed = live.events.filter((e) => e.type === "stepCompleted").length;
  let failedAt = -1;
  for (const e of live.events) {
    if (e.type === "stepCompleted" && !e.ok) {
      failedAt = e.stepIndex;
      break;
    }
  }
  return { totalSteps: started, completedSteps: completed, failedAt };
}

// Suppress unused-React-import false positive for hooks used elsewhere.
void useEffect;
void useState;
