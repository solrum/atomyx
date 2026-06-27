export type MirrorWindowMode = "compact" | "inspector" | "full";

export type MirrorDock = "free" | "right";

export interface MirrorWindowPosition {
  readonly x: number;
  readonly y: number;
}

export interface MirrorWindowSnapshot {
  readonly isOpen: boolean;
  readonly mode: MirrorWindowMode;
  readonly dock: MirrorDock;
  readonly position: MirrorWindowPosition;
  readonly scrubStep: number | null;
}

export interface MirrorWindowApi {
  getSnapshot(): MirrorWindowSnapshot;
  subscribe(listener: () => void): () => void;
  toggle(): void;
  open(): void;
  close(): void;
  setMode(mode: MirrorWindowMode): void;
  setDock(dock: MirrorDock): void;
  setPosition(pos: MirrorWindowPosition): void;
  setScrubStep(step: number | null): void;
}
