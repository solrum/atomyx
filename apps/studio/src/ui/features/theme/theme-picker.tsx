import { Palette } from "lucide-react";
import { useThemes } from "../../../state/features/theme/index.js";

/**
 * Inline dropdown in the header that swaps the active Atomyx
 * theme at runtime. Reads the list + active id from the
 * `theme.store`, which merges built-ins with the user's theme
 * folder under `Application Support`.
 */
export function ThemePicker() {
  const { available, activeId, setActiveId } = useThemes();
  return (
    <label className="flex items-center gap-1 text-xs text-neutral-400">
      <Palette className="h-3 w-3" />
      <select
        value={activeId ?? ""}
        onChange={(e) => void setActiveId(e.target.value)}
        className="bg-neutral-800 text-neutral-100 border border-neutral-700 rounded px-2 py-1 text-xs"
      >
        {available.map((t) => (
          <option key={t.id} value={t.id}>
            {t.label}
          </option>
        ))}
      </select>
    </label>
  );
}
