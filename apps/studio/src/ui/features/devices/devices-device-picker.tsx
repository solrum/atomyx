import { useEffect } from "react";
import { Smartphone, RotateCcw } from "lucide-react";
import { useDevices } from "../../../state/features/devices/index.js";

export function DevicePicker() {
  const { devices, selectedId, loading, error, refresh, select } = useDevices();

  useEffect(() => {
    if (devices.length === 0 && !loading && error === null) {
      void refresh();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const selected = devices.find((d) => d.id === selectedId);
  const label = selected
    ? `${selected.name}${selected.platform === "android" ? " (Android)" : " (iOS)"}`
    : loading
      ? "Scanning…"
      : devices.length === 0
        ? "No device"
        : "Select device";

  return (
    <div className="flex items-center gap-1">
      <Smartphone
        className="h-3 w-3"
        style={{ color: "var(--fg-2)" }}
      />
      <select
        value={selectedId ?? ""}
        onChange={(e) => void select(e.target.value || null)}
        disabled={devices.length === 0}
        className="text-xs rounded px-1 py-0.5 min-w-[140px]"
        style={{
          background: "var(--bg-hover)",
          color: "var(--fg-0)",
          border: "1px solid var(--line)",
        }}
        title={error ?? label}
      >
        {devices.length === 0 ? (
          <option value="">{label}</option>
        ) : (
          devices.map((d) => (
            <option key={d.id} value={d.id}>
              {d.name}
              {d.platform === "android" ? " · Android" : " · iOS"}
            </option>
          ))
        )}
      </select>
      <button
        type="button"
        aria-label="Refresh devices"
        onClick={() => void refresh()}
        disabled={loading}
        className="opacity-60 hover:opacity-100"
        title="Refresh devices"
      >
        <RotateCcw className="h-3 w-3" />
      </button>
    </div>
  );
}
