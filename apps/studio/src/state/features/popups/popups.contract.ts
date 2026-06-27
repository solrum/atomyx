export type PopupId = string;

export interface PopupsSnapshot {
  readonly openIds: ReadonlySet<PopupId>;
}

export interface PopupsApi {
  getSnapshot(): PopupsSnapshot;
  subscribe(listener: () => void): () => void;
  open(id: PopupId): void;
  close(id: PopupId): void;
  closeAll(): void;
  isOpen(id: PopupId): boolean;
}
