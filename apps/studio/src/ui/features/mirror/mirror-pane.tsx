import { useEffect, useMemo, useRef, useState } from "react";

import { getFeature } from "../../../state/core/registry.js";
import type { IosAgentApi } from "../../../state/features/ios-agent/index.js";
import { IOS_AGENT_KEY, useIosAgentStatus } from "../../../state/features/ios-agent/index.js";
import type { AndroidAgentApi } from "../../../state/features/android-agent/index.js";
import {
  ANDROID_AGENT_KEY,
  useAndroidAgentStatus,
} from "../../../state/features/android-agent/index.js";
import { useMirror } from "../../../state/features/mirror/index.js";
import type {
  MirrorApi,
  MirrorSessionStatus,
  MirrorTouchSink,
} from "../../../state/features/mirror/index.js";
import { MIRROR_KEY } from "../../../state/features/mirror/index.js";


import { attachAnnexBPath } from "./mirror-annexb-decoder.js";
import { attachFmp4Path } from "./mirror-fmp4-demuxer.js";
import { InspectorOverlay } from "./inspector-overlay.js";
import { clientToDeviceRatio, eventToDevicePoint } from "./mirror-pointer.js";
import { MirrorToolbar } from "./mirror-toolbar.js";

// Device-side duration the synthesised pinch plays over. Independent
// of the wall-clock length of the user's trackpad gesture — the pane
// dispatches one pinch from scale 1.0 to the gesture's end scale.
const PINCH_DURATION_MS = 250;

// Minimum |endScale − 1| before a trackpad gesture is treated as a
// pinch. Filters out incidental sub-threshold scale jitter so a
// two-finger scroll or tap does not emit a no-op pinch.
const PINCH_MIN_SCALE_DELTA = 0.05;

/**
 * WebKit-only trackpad gesture event (Safari / WKWebView). Not in the
 * standard DOM lib; only the fields the pinch path reads are typed.
 */
interface WebKitGestureEvent extends Event {
  readonly scale: number;
  readonly clientX: number;
  readonly clientY: number;
}

export interface MirrorPaneProps {
  /**
   * When true the inline toolbar (record / dimensions chip etc.) is
   * suppressed and the canvas fills the full available area. Hosts
   * that already provide their own chrome (e.g. the mirror window's
   * titlebar) pass this so the embedded canvas does not consume
   * vertical space and break the parent's aspect contract — when
   * canvas region aspect drifts away from the encoded video aspect
   * the browser letterboxes the frame, which silently shifts every
   * subsequent tap by the letterbox offset.
   */
  readonly hideToolbar?: boolean;
}

export function MirrorPane({ hideToolbar = false }: MirrorPaneProps = {}) {
  const snapshot = useMirror();
  const sessions = useMemo(
    () => Object.values(snapshot.sessions),
    [snapshot.sessions],
  );

  if (sessions.length === 0) {
    return (
      <div
        className="px-3 py-2 text-xs"
        style={{ color: "var(--fg-2)" }}
      >
        No active mirror session. Start one from the Mirror panel or the device
        picker.
      </div>
    );
  }

  const session = sessions[0]!;
  return <MirrorSessionView session={session} hideToolbar={hideToolbar} />;
}

function MirrorSessionView({
  session,
  hideToolbar,
}: {
  readonly session: MirrorSessionStatus;
  readonly hideToolbar: boolean;
}) {
  const mirrorApi = getFeature<MirrorApi>(MIRROR_KEY);
  const canvasRef = useRef<HTMLCanvasElement>(null);
  // Hidden capture field for physical-keyboard forwarding. Focused
  // when the user taps the canvas; keystrokes typed while it holds
  // focus are forwarded to the device's focused field.
  const keyboardRef = useRef<HTMLTextAreaElement>(null);
  const [decodeError, setDecodeError] = useState<string | null>(null);
  const [unsupported, setUnsupported] = useState(false);
  // Real video frame dimensions reported by the decoder. The
  // session header carries an SCK placeholder (1080×1080) for iOS
  // simulator captures, so we cannot rely on it for the canvas
  // aspect ratio — that would render the portrait stream into a
  // square box and break the inspector overlay's letterbox math.
  const [frameDims, setFrameDims] = useState<{
    readonly width: number;
    readonly height: number;
  } | null>(null);

  // Boot the XCUITest agent when an iOS mirror session is active.
  // Covers the case where the session was started in a previous
  // webview load — the Rust mirror session persists across frontend
  // reloads but the agent supervisor state lives in the sidecar and
  // must be re-ensured after any hard reload.
  useEffect(() => {
    const kind = session.target.kind;
    if (kind !== "ios-simulator" && kind !== "ios-device") return;
    const agentKind = kind === "ios-simulator" ? "simulator" : "device";
    const api = getFeature<IosAgentApi>(IOS_AGENT_KEY);
    void api.ensure(session.target.id, agentKind).catch((err) => {
      console.error("[mirror-pane] iosAgent.ensure failed", err);
    });
    const stopPoll = api.startPolling(session.target.id, 2_000);
    return () => stopPoll();
  }, [session.target.id, session.target.kind]);

  // Start polling the Android agent when an Android mirror session is
  // active so the readiness gate below stays current.
  useEffect(() => {
    if (session.target.kind !== "android") return;
    const api = getFeature<AndroidAgentApi>(ANDROID_AGENT_KEY);
    void api.ensure(session.target.id).catch((err) => {
      console.error("[mirror-pane] androidAgent.ensure failed", err);
    });
    const stopPoll = api.startPolling(session.target.id, 2_000);
    return () => stopPoll();
  }, [session.target.id, session.target.kind]);

  const isIos =
    session.target.kind === "ios-simulator" ||
    session.target.kind === "ios-device";
  const iosStatus = useIosAgentStatus(isIos ? session.target.id : null);
  const androidStatus = useAndroidAgentStatus(
    session.target.kind === "android" ? session.target.id : null,
  );
  const isReady =
    (isIos && iosStatus?.state === "ready") ||
    (session.target.kind === "android" && androidStatus?.state === "ready");

  const agentMessage = isIos ? iosStatus?.message : androidStatus?.message;

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const VD = (globalThis as unknown as { VideoDecoder?: typeof VideoDecoder })
      .VideoDecoder;
    if (!VD) {
      setUnsupported(true);
      return;
    }
    const ctx = canvas.getContext("2d", { alpha: false });
    if (!ctx) return;

    let decoderConfigured = false;
    const decoder = new VD({
      output: (frame) => {
        if (canvas.width !== frame.displayWidth) canvas.width = frame.displayWidth;
        if (canvas.height !== frame.displayHeight) canvas.height = frame.displayHeight;
        setFrameDims((prev) =>
          prev?.width === frame.displayWidth &&
          prev?.height === frame.displayHeight
            ? prev
            : { width: frame.displayWidth, height: frame.displayHeight },
        );
        // Push the real encoded dimensions back to the mirror store
        // so other consumers (phone-frame chrome, inspector overlay)
        // get an aspect-accurate session — the SCK adapter ships a
        // 1080×1080 placeholder before decode lands.
        mirrorApi.setSessionDims(
          session.id,
          frame.displayWidth,
          frame.displayHeight,
        );
        ctx.drawImage(
          frame as unknown as CanvasImageSource,
          0,
          0,
          canvas.width,
          canvas.height,
        );
        frame.close();
      },
      error: (e) => {
        console.error("[mirror] VideoDecoder error", e);
        setDecodeError(e.message);
      },
    });

    const configure = (config: VideoDecoderConfig) => {
      try {
        decoder.configure(config);
        decoderConfigured = true;
      } catch (e) {
        console.error("[mirror] configure failed", e);
        setDecodeError(
          e instanceof Error ? e.message : "VideoDecoder.configure failed",
        );
      }
    };

    // iOS Simulator sessions now deliver Annex-B NAL units via WebSocket
    // (VT+WS transport). Route them through attachAnnexBPath, which
    // handles SPS/PPS config frames and IDR/delta NAL dispatch.
    // The fMP4 demuxer is retained for the developer rollback case
    // (ATOMYX_MIRROR_BACKEND=fmp4 on the helper side); it is not
    // reachable from production webview builds.
    const cleanup = session.backend === "scrcpy"
      ? attachFmp4Path(mirrorApi, session.id, decoder, configure)
      : attachAnnexBPath(mirrorApi, session.id, decoder, configure, () =>
          decoderConfigured,
        );

    return () => {
      cleanup();
      try {
        decoder.close();
      } catch {
        // already closed
      }
    };
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [session.id, session.backend]);

  const touchEnabled = session.capabilities.supportsTouch;

  const gestureRef = useRef<{
    readonly start: { readonly x: number; readonly y: number };
    readonly canvasStart: { readonly clientX: number; readonly clientY: number };
    readonly startedAt: number;
    readonly srcWidth: number;
    readonly srcHeight: number;
  } | null>(null);

  // The adapter chose its own touch dispatch strategy via
  // `createTouchSink`. The pane forwards pointer-lifecycle events
  // unchanged; whether the result is a streamed `down → move → up`
  // (scrcpy) or a classified tap / swipe / long-press dispatch
  // (simctl) is the sink's concern.
  const sinkRef = useRef<MirrorTouchSink | null>(null);
  if (sinkRef.current === null) {
    sinkRef.current = mirrorApi.createTouchSink(session.id);
  }
  const sink = sinkRef.current;

  const onPointerDown = (e: React.PointerEvent<HTMLCanvasElement>) => {
    if (!touchEnabled || !isReady) return;
    if (e.button !== 0) return;
    // Suppress the default mousedown focus handling that otherwise
    // blurs our hidden capture field back to <body> the instant we
    // focus it — without this, physical keystrokes never reach the
    // textarea. Pointer capture and our gesture handlers are
    // unaffected (they run from this handler, not the default action).
    e.preventDefault();
    e.currentTarget.setPointerCapture(e.pointerId);
    // Take keyboard focus so subsequent physical keystrokes forward
    // to the device (the user has just tapped — likely into a field).
    keyboardRef.current?.focus({ preventScroll: true });
    const { x, y, srcWidth, srcHeight } = eventToDevicePoint(e, session);
    gestureRef.current = {
      start: { x, y },
      canvasStart: { clientX: e.clientX, clientY: e.clientY },
      startedAt: performance.now(),
      srcWidth,
      srcHeight,
    };
    void sink
      .beginPress({ x, y, srcWidth, srcHeight })
      .catch((err) => console.error("[mirror-pane] beginPress failed", err));
  };

  const onPointerMove = (e: React.PointerEvent<HTMLCanvasElement>) => {
    if (!touchEnabled || !isReady) return;
    if (gestureRef.current === null) return;
    const { x, y, srcWidth, srcHeight } = eventToDevicePoint(e, session);
    void sink
      .trackTo({ x, y, srcWidth, srcHeight })
      .catch((err) => console.error("[mirror-pane] trackTo failed", err));
  };

  const onPointerUp = async (e: React.PointerEvent<HTMLCanvasElement>) => {
    if (!touchEnabled || !isReady) return;
    const g = gestureRef.current;
    gestureRef.current = null;
    if (!g) return;
    try {
      e.currentTarget.releasePointerCapture(e.pointerId);
    } catch {
      // already released
    }
    const { x, y } = eventToDevicePoint(e, session);
    const dxClient = e.clientX - g.canvasStart.clientX;
    const dyClient = e.clientY - g.canvasStart.clientY;
    const displacementPx = Math.hypot(dxClient, dyClient);
    const heldMs = performance.now() - g.startedAt;
    try {
      await sink.endPress(
        { x, y, srcWidth: g.srcWidth, srcHeight: g.srcHeight },
        heldMs,
        displacementPx,
      );
    } catch (err) {
      console.error("[mirror-pane] endPress failed", err);
    }
  };

  // Trackpad pinch (WKWebView gesture events). These are native, not
  // React synthetic events, so they are attached imperatively. A
  // pinch is dispatched as a one-shot two-finger gesture on
  // gestureend, scaling from 1.0 to the gesture's end scale around
  // the point where the pinch began. Live state (readiness, the
  // current session shape) is read through refs so the listeners
  // stay attached for the session's lifetime.
  const isReadyRef = useRef(isReady);
  isReadyRef.current = isReady;
  const sessionRef = useRef(session);
  sessionRef.current = session;

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    let active = false;
    let center = { xRatio: 0.5, yRatio: 0.5 };
    let lastScale = 1;

    const onGestureStart = (ev: Event) => {
      const s = sessionRef.current;
      if (!s.capabilities.supportsPinch || !isReadyRef.current) return;
      ev.preventDefault();
      const g = ev as WebKitGestureEvent;
      active = true;
      lastScale = g.scale || 1;
      center = clientToDeviceRatio(canvas, g.clientX, g.clientY, s);
    };
    const onGestureChange = (ev: Event) => {
      if (!active) return;
      ev.preventDefault();
      lastScale = (ev as WebKitGestureEvent).scale || lastScale;
    };
    const onGestureEnd = (ev: Event) => {
      if (!active) return;
      ev.preventDefault();
      active = false;
      const toScale = lastScale;
      if (Math.abs(toScale - 1) < PINCH_MIN_SCALE_DELTA) return;
      void mirrorApi
        .pinch(sessionRef.current.id, center, 1, toScale, PINCH_DURATION_MS)
        .catch((err) => console.error("[mirror-pane] pinch failed", err));
    };

    canvas.addEventListener("gesturestart", onGestureStart);
    canvas.addEventListener("gesturechange", onGestureChange);
    canvas.addEventListener("gestureend", onGestureEnd);
    return () => {
      canvas.removeEventListener("gesturestart", onGestureStart);
      canvas.removeEventListener("gesturechange", onGestureChange);
      canvas.removeEventListener("gestureend", onGestureEnd);
    };
  }, [session.id, mirrorApi]);

  // Physical-keyboard forwarding via a hidden capture textarea.
  //
  // The textarea is the source-of-truth buffer: the OS (including the
  // IME) edits it natively, so composition, replacement (Telex
  // "ee"→"ê"), autocorrect, and paste all resolve there first. On
  // every committed change we diff the field value against what we
  // have already sent to the device — shared prefix is kept, the
  // diverging tail is erased, and the new tail is typed. This avoids
  // forwarding intermediate composition states (which previously
  // produced doubled output like "eê").
  useEffect(() => {
    const ta = keyboardRef.current;
    if (!ta) return;
    let composing = false;
    // Mirror of what the device's field currently holds from our
    // forwarding, since the textarea last reset (focus / blur / Enter).
    let sent = "";

    const ready = () =>
      sessionRef.current.capabilities.supportsLiveTyping && isReadyRef.current;

    const reset = () => {
      ta.value = "";
      sent = "";
    };

    const syncToDevice = () => {
      if (!ready()) return;
      const value = ta.value;
      if (value === sent) return;
      let prefix = 0;
      const max = Math.min(value.length, sent.length);
      while (prefix < max && value[prefix] === sent[prefix]) prefix++;
      const eraseCount = sent.length - prefix;
      const insert = value.slice(prefix);
      sent = value;
      const id = sessionRef.current.id;
      if (eraseCount > 0) {
        void mirrorApi
          .eraseText(id, eraseCount)
          .catch((err) => console.error("[mirror-pane] eraseText failed", err));
      }
      if (insert) {
        void mirrorApi
          .inputText(id, insert)
          .catch((err) => console.error("[mirror-pane] inputText failed", err));
      }
    };

    const onCompositionStart = () => {
      composing = true;
    };
    const onCompositionEnd = () => {
      composing = false;
      syncToDevice();
    };
    const onInput = () => {
      if (composing) return;
      syncToDevice();
    };
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.isComposing || e.keyCode === 229) return;
      if (!ready()) return;
      if (e.key === "Enter") {
        // Submit semantics; the device may clear/commit the field, so
        // drop our local mirror and let the textarea start fresh.
        e.preventDefault();
        void mirrorApi
          .pressKey(sessionRef.current.id, "enter")
          .catch((err) => console.error("[mirror-pane] pressKey failed", err));
        reset();
      } else if (e.key === "Backspace" && ta.value.length === 0) {
        // Nothing buffered locally — forward a delete so the user can
        // erase content the field already held. When the buffer is
        // non-empty the textarea edits itself and syncToDevice diffs.
        e.preventDefault();
        void mirrorApi
          .eraseText(sessionRef.current.id, 1)
          .catch((err) => console.error("[mirror-pane] eraseText failed", err));
      }
    };
    const onBlur = () => reset();

    ta.addEventListener("compositionstart", onCompositionStart);
    ta.addEventListener("compositionend", onCompositionEnd);
    ta.addEventListener("input", onInput);
    ta.addEventListener("keydown", onKeyDown);
    ta.addEventListener("blur", onBlur);
    return () => {
      ta.removeEventListener("compositionstart", onCompositionStart);
      ta.removeEventListener("compositionend", onCompositionEnd);
      ta.removeEventListener("input", onInput);
      ta.removeEventListener("keydown", onKeyDown);
      ta.removeEventListener("blur", onBlur);
    };
  }, [session.id, mirrorApi]);

  return (
    <div className="flex h-full flex-col">
      {hideToolbar ? null : <MirrorToolbar session={session} />}
      <div className="flex-1 bg-black flex items-center justify-center overflow-hidden">
        <div className="relative inline-block max-w-full max-h-full">
          <canvas
            ref={canvasRef}
            className="touch-none select-none block"
            style={{
              maxWidth: "100%",
              maxHeight: "100%",
              width: "auto",
              height: "auto",
              objectFit: "contain",
              aspectRatio: frameDims
                ? `${frameDims.width} / ${frameDims.height}`
                : session.videoWidth && session.videoHeight
                  ? `${session.videoWidth} / ${session.videoHeight}`
                  : undefined,
            }}
            onPointerDown={onPointerDown}
            onPointerMove={onPointerMove}
            onPointerUp={onPointerUp}
            onPointerCancel={onPointerUp}
            onContextMenu={(e) => e.preventDefault()}
          />
          <textarea
            ref={keyboardRef}
            aria-hidden
            tabIndex={-1}
            autoCapitalize="off"
            autoCorrect="off"
            autoComplete="off"
            spellCheck={false}
            className="absolute top-0 left-0 h-px w-px resize-none border-0 p-0 opacity-0"
            style={{ pointerEvents: "none" }}
          />
          <InspectorOverlay canvasRef={canvasRef} session={session} />
          {!isReady ? (
            <div
              className="absolute inset-0 bg-black/40 flex items-center justify-center text-white text-sm"
              style={{ pointerEvents: "auto" }}
            >
              <span>{agentMessage ?? "Connecting…"}</span>
            </div>
          ) : null}
        </div>
      </div>
      {unsupported ? <Banner>This browser lacks WebCodecs.</Banner> : null}
      {decodeError ? <Banner>Decoder error: {decodeError}</Banner> : null}
    </div>
  );
}

function Banner({ children }: { readonly children: React.ReactNode }) {
  return (
    <div
      className="px-3 py-1 text-xs"
      style={{ color: "var(--fg-2)" }}
    >
      {children}
    </div>
  );
}
