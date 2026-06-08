import type { ScreenMirror, TouchEvent } from "./mirror.port.js";
import type {
  ClipOptions,
  H264Frame,
  MirrorOptions,
  MirrorSession,
  MirrorTarget,
  RecordingHandle,
  Unsubscribe,
} from "./mirror.types.js";
import type { MirrorTouchSink } from "./mirror-touch-sink.js";

interface MockSessionState {
  readonly session: MirrorSession;
  readonly frameListeners: Set<(frame: H264Frame) => void>;
  readonly touches: TouchEvent[];
  recording?: RecordingHandle;
}

/**
 * In-memory ScreenMirror used by state + UI tests. Consumers start
 * a session, push synthetic frames via `feedFrame`, and assert
 * listeners received them. No subprocess, no platform assumptions.
 */
let mockInstanceSeq = 0;

export class MockScreenMirror implements ScreenMirror {
  private sessionSeq = 0;
  private readonly instanceId = ++mockInstanceSeq;
  private readonly sessions = new Map<string, MockSessionState>();
  private readonly now: () => number;

  constructor(deps: { now?: () => number } = {}) {
    this.now = deps.now ?? (() => Date.now());
  }

  async start(
    target: MirrorTarget,
    _opts: MirrorOptions = {},
  ): Promise<MirrorSession> {
    const id = `mock-${this.instanceId}-${++this.sessionSeq}`;
    const session: MirrorSession = {
      id,
      target,
      startedAt: this.now(),
      backend: "mock",
      videoWidth: 360,
      videoHeight: 640,
      capabilities: {
        supportsRecording: true,
        supportsTouch: true,
        supportsKeyboard: true,
        supportsLiveTyping: true,
        supportsPinch: true,
      },
    };
    this.sessions.set(id, {
      session,
      frameListeners: new Set(),
      touches: [],
    });
    return session;
  }

  async stop(session: MirrorSession): Promise<void> {
    this.sessions.delete(session.id);
  }

  onFrame(
    session: MirrorSession,
    listener: (frame: H264Frame) => void,
  ): Unsubscribe {
    const state = this.requireSession(session.id);
    state.frameListeners.add(listener);
    return () => state.frameListeners.delete(listener);
  }

  async record(
    session: MirrorSession,
    outputPath: string,
  ): Promise<RecordingHandle> {
    const state = this.requireSession(session.id);
    const handle: RecordingHandle = { session, outputPath };
    state.recording = handle;
    return handle;
  }

  async stopRecording(handle: RecordingHandle): Promise<void> {
    const state = this.requireSession(handle.session.id);
    state.recording = undefined;
  }

  async clipFromRecording(
    _handle: RecordingHandle,
    opts: ClipOptions,
  ): Promise<string> {
    return opts.outputPath;
  }

  async sendTouch(session: MirrorSession, event: TouchEvent): Promise<void> {
    const state = this.requireSession(session.id);
    state.touches.push(event);
  }

  async longPressAt(): Promise<void> {
    // Mock no-op — gesture flow is exercised end-to-end via real
    // adapters; mock callers that need to assert long-press
    // dispatch should swap in a richer fake.
  }

  async swipe(): Promise<void> {
    // Mock no-op — see longPressAt note above.
  }

  async pinch(): Promise<void> {
    // Mock no-op — see longPressAt note above.
  }

  async inputText(): Promise<void> {
    // Mock no-op — see longPressAt note above.
  }

  async eraseText(): Promise<void> {
    // Mock no-op — see longPressAt note above.
  }

  async pressKey(): Promise<void> {
    // Mock no-op — see longPressAt note above.
  }

  createTouchSink(session: MirrorSession): MirrorTouchSink {
    // Default fake routes through this mock's own `sendTouch` so
    // tests that assert injected events keep working with the
    // canvas-side touch-sink path. Tests needing classified-
    // gesture semantics can swap the returned sink for their own
    // double via `MockScreenMirror`'s setter (not present here —
    // add when the first test demands it).
    return {
      beginPress: async (point) => {
        await this.sendTouch(session, {
          action: "down",
          x: point.x,
          y: point.y,
          pressure: 1,
          srcWidth: point.srcWidth,
          srcHeight: point.srcHeight,
        });
      },
      trackTo: async (point) => {
        await this.sendTouch(session, {
          action: "move",
          x: point.x,
          y: point.y,
          pressure: 1,
          srcWidth: point.srcWidth,
          srcHeight: point.srcHeight,
        });
      },
      endPress: async (point) => {
        await this.sendTouch(session, {
          action: "up",
          x: point.x,
          y: point.y,
          pressure: 0,
          srcWidth: point.srcWidth,
          srcHeight: point.srcHeight,
        });
      },
    };
  }

  /** Test helper — read back injected touches. */
  recordedTouches(sessionId: string): readonly TouchEvent[] {
    return this.requireSession(sessionId).touches;
  }

  /** Test helper: push a synthetic frame to every attached listener. */
  feedFrame(sessionId: string, frame: H264Frame): void {
    const state = this.requireSession(sessionId);
    for (const listener of state.frameListeners) {
      listener(frame);
    }
  }

  hasRecording(sessionId: string): boolean {
    return Boolean(this.sessions.get(sessionId)?.recording);
  }

  activeSessions(): readonly MirrorSession[] {
    return [...this.sessions.values()].map((s) => s.session);
  }

  private requireSession(sessionId: string): MockSessionState {
    const state = this.sessions.get(sessionId);
    if (!state) {
      throw new Error(
        `MockScreenMirror: session ${sessionId} not found — call start() first.`,
      );
    }
    return state;
  }
}
