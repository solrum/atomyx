/**
 * Format `ts` (epoch ms) as a coarse "N <unit> ago" label,
 * always relative to `now` (defaults to wall-clock). Tests pass a
 * fixed `now` so the buckets can be verified deterministically.
 *
 * Buckets:
 *   - < 60s:        "<n>s ago"
 *   - < 60min:      "<n>m ago"
 *   - < 24h:        "<n>h ago"
 *   - otherwise:    "<n>d ago"
 *
 * Future timestamps (ts > now) clamp to "0s ago" so a clock skew
 * does not produce negative durations in the UI.
 */
export function formatRelativeTime(ts: number, now: number = Date.now()): string {
  const diff = Math.max(0, now - ts);
  const s = Math.floor(diff / 1000);
  if (s < 60) return `${s}s ago`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  const d = Math.floor(h / 24);
  return `${d}d ago`;
}
