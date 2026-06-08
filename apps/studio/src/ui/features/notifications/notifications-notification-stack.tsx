import { CheckCircle2, Info, X, AlertTriangle, AlertOctagon } from "lucide-react";
import { useNotifications } from "../../../state/features/notifications/index.js";
import type {
  Notification,
  NotificationKind,
} from "../../../state/features/notifications/index.js";

/**
 * Bottom-right stacked toasts. Auto-dismiss by TTL unless pinned;
 * pinned toasts live until the user clicks the close button. Long
 * tasks post a toast with `progress` populated and update it via
 * the NotificationsApi.update method as they progress.
 */
export function NotificationStack() {
  const { items, dismiss } = useNotifications();
  if (items.length === 0) return null;
  return (
    <div className="fixed bottom-4 right-4 z-50 flex flex-col gap-2 w-80">
      {items.slice(-5).map((n) => (
        <NotificationCard
          key={n.id}
          notification={n}
          onDismiss={() => dismiss(n.id)}
        />
      ))}
    </div>
  );
}

function NotificationCard({
  notification,
  onDismiss,
}: {
  readonly notification: Notification;
  readonly onDismiss: () => void;
}) {
  return (
    <div
      className="rounded-md shadow-lg overflow-hidden"
      style={{
        background: "var(--bg-2)",
        border: "1px solid var(--line)",
      }}
    >
      <div className="flex items-start gap-2 px-3 py-2">
        <KindIcon kind={notification.kind} />
        <div className="flex-1 min-w-0">
          <div className="text-sm" style={{ color: "var(--fg-0)" }}>
            {notification.title}
          </div>
          {notification.detail ? (
            <div
              className="text-xs mt-0.5"
              style={{ color: "var(--fg-2)" }}
            >
              {notification.detail}
            </div>
          ) : null}
        </div>
        <button
          type="button"
          onClick={onDismiss}
          className="opacity-60 hover:opacity-100"
          aria-label="Dismiss"
        >
          <X className="h-3 w-3" />
        </button>
      </div>
      {typeof notification.progress === "number" ? (
        <div
          className="h-0.5"
          style={{ background: "var(--line)" }}
        >
          <div
            className="h-full transition-all"
            style={{
              width: `${Math.max(0, Math.min(1, notification.progress)) * 100}%`,
              background: "var(--accent)",
            }}
          />
        </div>
      ) : null}
    </div>
  );
}

function KindIcon({ kind }: { readonly kind: NotificationKind }) {
  const className = "h-4 w-4 flex-shrink-0 mt-0.5";
  const color = colorFor(kind);
  switch (kind) {
    case "info":
      return <Info className={className} style={{ color }} />;
    case "success":
      return <CheckCircle2 className={className} style={{ color }} />;
    case "warn":
      return <AlertTriangle className={className} style={{ color }} />;
    case "error":
      return <AlertOctagon className={className} style={{ color }} />;
  }
}

function colorFor(kind: NotificationKind): string {
  switch (kind) {
    case "info":
      return "var(--diagnostic-info-fg)";
    case "success":
      return "var(--ok)";
    case "warn":
      return "var(--diagnostic-warning-fg)";
    case "error":
      return "var(--err)";
  }
}
