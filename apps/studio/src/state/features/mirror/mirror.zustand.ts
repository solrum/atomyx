import { createStore } from "zustand/vanilla";

import type { ScreenMirror } from "../../../domain/features/mirror/mirror.port.js";
import type {
  H264Frame,
  MirrorOptions,
  MirrorSession,
  MirrorTarget,
  RecordingHandle,
} from "../../../domain/features/mirror/mirror.types.js";

import type {
  ClipRequest,
  MirrorApi,
  MirrorSessionStatus,
  MirrorSnapshot,
  MirrorTouchEvent,
  MirrorTouchSink,
} from "./mirror.contract.js";

interface InternalSessionState {
  readonly session: MirrorSession;
  readonly frameListeners: Set<(frame: H264Frame) => void>;
  unsubFrames: () => void;
  recordingHandle: RecordingHandle | null;
  frameCount: number;
}

export function createZustandMirror(deps: {
  readonly port: ScreenMirror;
  /**
   * Fires once per completed user gesture (tap end, long-press
   * release, swipe release, classified `sendTouch`). Wired to the
   * inspector's `notifyInteraction` so auto-refresh skips a tick
   * while the device UI settles.
   */
  readonly onInteraction?: () => void;
}): MirrorApi {
  const { port, onInteraction } = deps;
  const notifyInteraction = (): void => {
    if (onInteraction) onInteraction();
  };

  const store = createStore<MirrorSnapshot>(() => ({ sessions: {} }));
  const byId = new Map<string, InternalSessionState>();

  const patchSession = (
    id: string,
    patch: Partial<MirrorSessionStatus>,
  ): void => {
    const current = store.getState().sessions[id];
    if (!current) return;
    store.setState({
      sessions: { ...store.getState().sessions, [id]: { ...current, ...patch } },
    });
  };

  const removeSession = (id: string): void => {
    const { [id]: _removed, ...rest } = store.getState().sessions;
    store.setState({ sessions: rest });
  };

  const requireSession = (sessionId: string): InternalSessionState => {
    const state = byId.get(sessionId);
    if (!state) {
      throw new Error(
        `MirrorApi: unknown session ${sessionId}. Call startForTarget() first.`,
      );
    }
    return state;
  };

  const startForTarget = async (
    target: MirrorTarget,
    opts: MirrorOptions = {},
  ): Promise<string> => {
    const session = await port.start(target, opts);

    const frameListeners = new Set<(frame: H264Frame) => void>();
    const internal: InternalSessionState = {
      session,
      frameListeners,
      unsubFrames: () => undefined,
      recordingHandle: null,
      frameCount: 0,
    };

    internal.unsubFrames = port.onFrame(session, (frame) => {
      internal.frameCount += 1;
      for (const l of frameListeners) l(frame);
    });

    byId.set(session.id, internal);

    store.setState({
      sessions: {
        ...store.getState().sessions,
        [session.id]: {
          id: session.id,
          target: session.target,
          startedAt: session.startedAt,
          backend: session.backend,
          isRecording: false,
          recordingPath: null,
          videoWidth: session.videoWidth,
          videoHeight: session.videoHeight,
          capabilities: session.capabilities,
        },
      },
    });

    return session.id;
  };

  const stop = async (sessionId: string): Promise<void> => {
    const state = byId.get(sessionId);
    if (!state) return;
    state.unsubFrames();
    await port.stop(state.session);
    byId.delete(sessionId);
    removeSession(sessionId);
  };

  const startRecording = async (
    sessionId: string,
    outputPath: string,
  ): Promise<void> => {
    const state = requireSession(sessionId);
    const handle = await port.record(state.session, outputPath);
    state.recordingHandle = handle;
    patchSession(sessionId, { isRecording: true, recordingPath: outputPath });
  };

  const stopRecording = async (sessionId: string): Promise<void> => {
    const state = requireSession(sessionId);
    const handle = state.recordingHandle;
    if (!handle) return;
    await port.stopRecording(handle);
    state.recordingHandle = null;
    patchSession(sessionId, { isRecording: false, recordingPath: null });
  };

  const extractClip = async (
    sessionId: string,
    opts: ClipRequest,
  ): Promise<string> => {
    const state = requireSession(sessionId);
    if (!state.recordingHandle) {
      throw new Error(
        `MirrorApi: session ${sessionId} has no active recording — call startRecording() before extractClip().`,
      );
    }
    return port.clipFromRecording(state.recordingHandle, opts);
  };

  const onFrame = (
    sessionId: string,
    listener: (frame: H264Frame) => void,
  ): (() => void) => {
    const state = requireSession(sessionId);
    state.frameListeners.add(listener);
    return () => state.frameListeners.delete(listener);
  };

  const sendTouch = async (
    sessionId: string,
    event: MirrorTouchEvent,
  ): Promise<void> => {
    const state = requireSession(sessionId);
    await port.sendTouch(state.session, event);
    notifyInteraction();
  };

  const longPressAt = async (
    sessionId: string,
    point: {
      readonly x: number;
      readonly y: number;
      readonly srcWidth?: number;
      readonly srcHeight?: number;
    },
    durationMs: number,
  ): Promise<void> => {
    const state = requireSession(sessionId);
    await port.longPressAt(state.session, point, durationMs);
    notifyInteraction();
  };

  const swipe = async (
    sessionId: string,
    from: {
      readonly x: number;
      readonly y: number;
      readonly srcWidth?: number;
      readonly srcHeight?: number;
    },
    to: { readonly x: number; readonly y: number },
    durationMs: number,
  ): Promise<void> => {
    const state = requireSession(sessionId);
    await port.swipe(state.session, from, to, durationMs);
    notifyInteraction();
  };

  const pinch = async (
    sessionId: string,
    center: { readonly xRatio: number; readonly yRatio: number },
    fromScale: number,
    toScale: number,
    durationMs: number,
  ): Promise<void> => {
    const state = requireSession(sessionId);
    await port.pinch(state.session, center, fromScale, toScale, durationMs);
    notifyInteraction();
  };

  const inputText = async (sessionId: string, text: string): Promise<void> => {
    const state = requireSession(sessionId);
    await port.inputText(state.session, text);
    notifyInteraction();
  };

  const eraseText = async (sessionId: string, count: number): Promise<void> => {
    const state = requireSession(sessionId);
    await port.eraseText(state.session, count);
    notifyInteraction();
  };

  const pressKey = async (sessionId: string, key: string): Promise<void> => {
    const state = requireSession(sessionId);
    await port.pressKey(state.session, key);
    notifyInteraction();
  };

  const createTouchSink = (sessionId: string): MirrorTouchSink => {
    const state = requireSession(sessionId);
    const inner = port.createTouchSink(state.session);
    return {
      beginPress: (point) => inner.beginPress(point),
      trackTo: (point) => inner.trackTo(point),
      endPress: async (point, heldMs, displacementPx) => {
        await inner.endPress(point, heldMs, displacementPx);
        notifyInteraction();
      },
    };
  };

  const setSessionDims = (
    sessionId: string,
    width: number,
    height: number,
  ): void => {
    if (width <= 0 || height <= 0) return;
    const current = store.getState().sessions[sessionId];
    if (!current) return;
    if (current.videoWidth === width && current.videoHeight === height) return;
    patchSession(sessionId, { videoWidth: width, videoHeight: height });
  };

  return {
    getSnapshot: () => store.getState(),
    subscribe: (l) => store.subscribe(l),
    startForTarget,
    stop,
    onFrame,
    startRecording,
    stopRecording,
    extractClip,
    sendTouch,
    longPressAt,
    swipe,
    pinch,
    inputText,
    eraseText,
    pressKey,
    createTouchSink,
    setSessionDims,
  };
}
