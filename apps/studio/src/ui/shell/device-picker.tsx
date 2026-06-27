import { useEffect } from "react";
import { ChevronDown, Smartphone } from "lucide-react";
import { useDevices } from "../../state/features/devices/index.js";

/**
 * Compact device dropdown for the title bar. Reads the live device
 * list + selection from `useDevices`; first refresh fires on mount
 * so the picker isn't empty when the user opens the app.
 */
export function DevicePicker() {
  const { devices, selectedId, loading, refresh, select } = useDevices();

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const selected = devices.find((d) => d.id === selectedId) ?? null;
  const label = loading
    ? "Loading…"
    : selected
      ? selected.name
      : "Select device";

  return (
    <label className="dp-picker" data-tauri-drag-region="false">
      <span className="dp-dot" aria-hidden style={{ background: selected ? "var(--ok)" : "var(--fg-3)" }} />
      <Smartphone style={{ width: 11, height: 11, color: "var(--fg-2)" }} />
      <span className="dp-label" title={selected?.platform ?? ""}>
        {label}
      </span>
      <ChevronDown style={{ width: 10, height: 10, color: "var(--fg-3)" }} />
      <select
        value={selectedId ?? ""}
        onChange={(e) => void select(e.target.value || null)}
        disabled={loading}
        aria-label="Select device"
      >
        <option value="">{loading ? "Loading…" : "Select device"}</option>
        {devices.map((d) => (
          <option key={d.id} value={d.id}>
            {d.name} ({d.platform})
          </option>
        ))}
      </select>
    </label>
  );
}
