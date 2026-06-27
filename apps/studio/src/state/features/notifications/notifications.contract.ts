export type NotificationKind = "info" | "success" | "warn" | "error";

export interface Notification {
  readonly id: string;
  readonly kind: NotificationKind;
  readonly title: string;
  readonly detail?: string;
  readonly createdAt: number;
  readonly ttlMs: number;
  readonly pinned: boolean;
  readonly progress?: number;
}

export interface NotificationsSnapshot {
  readonly items: readonly Notification[];
}

export interface ShowNotificationInput {
  readonly kind: NotificationKind;
  readonly title: string;
  readonly detail?: string;
  readonly progress?: number;
  readonly ttlMs?: number;
  readonly pinned?: boolean;
}

export interface NotificationsApi {
  getSnapshot(): NotificationsSnapshot;
  subscribe(listener: () => void): () => void;
  show(input: ShowNotificationInput): string;
  update(id: string, patch: Partial<Notification>): void;
  dismiss(id: string): void;
  clear(): void;
}
