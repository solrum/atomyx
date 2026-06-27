import type {
  ScreenMirror,
  TouchEvent,
} from "../../../domain/features/mirror/mirror.port.js";
import type {
  ClipOptions,
  H264Frame,
  MirrorOptions,
  MirrorSession,
  MirrorTarget,
  MirrorTargetKind,
  RecordingHandle,
  Unsubscribe,
} from "../../../domain/features/mirror/mirror.types.js";
import type { MirrorTouchSink } from "../../../domain/features/mirror/mirror-touch-sink.js";

/**
 * Routes `ScreenMirror` calls to the adapter registered for each
 * target kind. Sessions remember which adapter produced them, so
 * `stop` / `onFrame` / `record` / `clipFromRecording` dispatch back
 * without the caller having to track the mapping.
 *
 * The dispatcher is what consumers (state layer, UI) see. Swapping
 * an adapter implementation — e.g. replacing the scrcpy wrapper with
 * a scrcpy-server.jar direct impl — means changing the construction
 * in the composition root; the dispatcher surface does not move.
 */
export class ScreenMirrorDispatcher implements ScreenMirror {
  private readonly adaptersByKind: ReadonlyMap<MirrorTargetKind, ScreenMirror>;
  private readonly adapterBySession = new Map<string, ScreenMirror>();

  constructor(
    adapters: Readonly<Partial<Record<MirrorTargetKind, ScreenMirror>>>,
  ) {
    this.adaptersByKind = new Map(
      Object.entries(adapters).filter(
        ([, v]) => v !== undefined,
      ) as [MirrorTargetKind, ScreenMirror][],
    );
  }

  async start(
    target: MirrorTarget,
    opts: MirrorOptions = {},
  ): Promise<MirrorSession> {
    const adapter = this.requireAdapter(target.kind);
    const session = await adapter.start(target, opts);
    this.adapterBySession.set(session.id, adapter);
    return session;
  }

  async stop(session: MirrorSession): Promise<void> {
    const adapter = this.requireAdapterForSession(session);
    await adapter.stop(session);
    this.adapterBySession.delete(session.id);
  }

  onFrame(
    session: MirrorSession,
    listener: (frame: H264Frame) => void,
  ): Unsubscribe {
    const adapter = this.requireAdapterForSession(session);
    return adapter.onFrame(session, listener);
  }

  async record(
    session: MirrorSession,
    outputPath: string,
  ): Promise<RecordingHandle> {
    const adapter = this.requireAdapterForSession(session);
    return adapter.record(session, outputPath);
  }

  async stopRecording(handle: RecordingHandle): Promise<void> {
    const adapter = this.requireAdapterForSession(handle.session);
    await adapter.stopRecording(handle);
  }

  async clipFromRecording(
    handle: RecordingHandle,
    opts: ClipOptions,
  ): Promise<string> {
    const adapter = this.requireAdapterForSession(handle.session);
    return adapter.clipFromRecording(handle, opts);
  }

  async sendTouch(session: MirrorSession, event: TouchEvent): Promise<void> {
    const adapter = this.requireAdapterForSession(session);
    await adapter.sendTouch(session, event);
  }

  async longPressAt(
    session: MirrorSession,
    point: { readonly x: number; readonly y: number; readonly srcWidth?: number; readonly srcHeight?: number },
    durationMs: number,
  ): Promise<void> {
    const adapter = this.requireAdapterForSession(session);
    await adapter.longPressAt(session, point, durationMs);
  }

  async swipe(
    session: MirrorSession,
    from: { readonly x: number; readonly y: number; readonly srcWidth?: number; readonly srcHeight?: number },
    to: { readonly x: number; readonly y: number },
    durationMs: number,
  ): Promise<void> {
    const adapter = this.requireAdapterForSession(session);
    await adapter.swipe(session, from, to, durationMs);
  }

  async pinch(
    session: MirrorSession,
    center: { readonly xRatio: number; readonly yRatio: number },
    fromScale: number,
    toScale: number,
    durationMs: number,
  ): Promise<void> {
    const adapter = this.requireAdapterForSession(session);
    await adapter.pinch(session, center, fromScale, toScale, durationMs);
  }

  async inputText(session: MirrorSession, text: string): Promise<void> {
    const adapter = this.requireAdapterForSession(session);
    await adapter.inputText(session, text);
  }

  async eraseText(session: MirrorSession, count: number): Promise<void> {
    const adapter = this.requireAdapterForSession(session);
    await adapter.eraseText(session, count);
  }

  async pressKey(session: MirrorSession, key: string): Promise<void> {
    const adapter = this.requireAdapterForSession(session);
    await adapter.pressKey(session, key);
  }

  createTouchSink(session: MirrorSession): MirrorTouchSink {
    const adapter = this.requireAdapterForSession(session);
    return adapter.createTouchSink(session);
  }

  private requireAdapter(kind: MirrorTargetKind): ScreenMirror {
    const adapter = this.adaptersByKind.get(kind);
    if (!adapter) {
      throw new Error(
        `ScreenMirrorDispatcher: no adapter registered for target kind "${kind}".`,
      );
    }
    return adapter;
  }

  private requireAdapterForSession(session: MirrorSession): ScreenMirror {
    const adapter = this.adapterBySession.get(session.id);
    if (!adapter) {
      throw new Error(
        `ScreenMirrorDispatcher: session ${session.id} is not tracked — was it produced by this dispatcher?`,
      );
    }
    return adapter;
  }
}
