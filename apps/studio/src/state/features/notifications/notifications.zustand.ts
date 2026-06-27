import { createStore } from "zustand/vanilla";
import type {
  Notification,
  NotificationsApi,
  NotificationsSnapshot,
  ShowNotificationInput,
} from "./notifications.contract.js";

const DEFAULT_TTL_MS = 5_000;

export function createZustandNotifications(): NotificationsApi {
  const store = createStore<NotificationsSnapshot>(() => ({ items: [] }));

  const dismiss = (id: string): void => {
    store.setState({
      items: store.getState().items.filter((n) => n.id !== id),
    });
  };

  const show = (input: ShowNotificationInput): string => {
    const id = Math.random().toString(36).slice(2);
    const note: Notification = {
      id,
      kind: input.kind,
      title: input.title,
      detail: input.detail,
      createdAt: Date.now(),
      ttlMs: input.ttlMs ?? DEFAULT_TTL_MS,
      pinned: input.pinned ?? false,
      progress: input.progress,
    };
    store.setState({ items: [...store.getState().items, note] });
    if (!note.pinned && note.ttlMs > 0) {
      setTimeout(() => dismiss(id), note.ttlMs);
    }
    return id;
  };

  return {
    getSnapshot: () => store.getState(),
    subscribe: (l) => store.subscribe(l),
    show,
    update: (id, patch) => {
      store.setState({
        items: store
          .getState()
          .items.map((n) => (n.id === id ? { ...n, ...patch } : n)),
      });
    },
    dismiss,
    clear: () => store.setState({ items: [] }),
  };
}
