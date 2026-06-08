import type { MirrorTouchSink } from "../../../domain/features/mirror/index.js";

/**
 * Touch sink for backends that cannot drive their target — the
 * CoreMedia mirror, for instance, is read-only because USB
 * accessory framing on a physical iOS device exposes only the
 * video stream and not an input channel. Returning a no-op sink
 * (rather than `null` / a thrown error) lets the canvas-side
 * consumer keep a single uniform path; the canvas already gates
 * pointer plumbing on `capabilities.supportsTouch`.
 */
export class NoopTouchSink implements MirrorTouchSink {
  async beginPress(): Promise<void> {}
  async trackTo(): Promise<void> {}
  async endPress(): Promise<void> {}
}
