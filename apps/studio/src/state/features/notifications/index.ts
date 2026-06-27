import { useSyncExternalStore } from "react";
import { getFeature } from "../../core/registry.js";
import type {
  Notification,
  NotificationKind,
  NotificationsApi,
  NotificationsSnapshot,
  ShowNotificationInput,
} from "./notifications.contract.js";
import { createZustandNotifications } from "./notifications.zustand.js";

export type {
  Notification,
  NotificationKind,
  NotificationsApi,
  NotificationsSnapshot,
  ShowNotificationInput,
};

export const NOTIFICATIONS_KEY = "notifications";

export function createNotifications(): NotificationsApi {
  return createZustandNotifications();
}

export function useNotifications(): NotificationsSnapshot &
  Pick<NotificationsApi, "show" | "update" | "dismiss" | "clear"> {
  const api = getFeature<NotificationsApi>(NOTIFICATIONS_KEY);
  const snap = useSyncExternalStore(api.subscribe, api.getSnapshot, api.getSnapshot);
  return {
    ...snap,
    show: api.show,
    update: api.update,
    dismiss: api.dismiss,
    clear: api.clear,
  };
}
