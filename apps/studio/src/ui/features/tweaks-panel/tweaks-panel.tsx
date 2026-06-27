import { useEffect, useState } from "react";
import { Sparkles, X as XIcon } from "lucide-react";
import { useLayout } from "../../../state/features/layout/index.js";

/**
 * Live appearance tweaks (theme polarity / density / accent hue).
 * Writes directly to `<html>` data attributes and the
 * `--accent-h` custom property so the design tokens recolour
 * without round-tripping through the persisted theme registry.
 *
 * Session preferences persist in localStorage under
 * `atomyx.tweaks.v1`. Settings → Theme remains the canonical
 * place to pick a Monaco-tied theme; this panel adjusts the
 * surface tokens that live above that.
 */

export type TweaksMode = "dark" | "light" | "system";
export type TweaksDensity = "compact" | "comfortable" | "spacious";

export interface TweaksState {
  readonly mode: TweaksMode;
  readonly density: TweaksDensity;
  readonly accent: AccentKey;
}

const STORAGE_KEY = "atomyx.tweaks.v1";

const DENSITY_ROW_H: Record<TweaksDensity, string> = {
  compact: "20px",
  comfortable: "26px",
  spacious: "30px",
};

interface AccentSpec {
  readonly name: string;
  readonly h: number;
}

export const ACCENTS = {
  cyan: { name: "Cyan", h: 220 },
  violet: { name: "Violet", h: 300 },
  amber: { name: "Amber", h: 60 },
  lime: { name: "Lime", h: 140 },
  coral: { name: "Coral", h: 30 },
  pink: { name: "Pink", h: 350 },
} as const satisfies Record<string, AccentSpec>;

export type AccentKey = keyof typeof ACCENTS;

const DEFAULT_STATE: TweaksState = {
  mode: "dark",
  density: "compact",
  accent: "cyan",
};

function readState(): TweaksState {
  if (typeof window === "undefined") return DEFAULT_STATE;
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (!raw) return DEFAULT_STATE;
    const parsed = JSON.parse(raw) as Partial<TweaksState>;
    return {
      mode:
        parsed.mode === "dark" ||
        parsed.mode === "light" ||
        parsed.mode === "system"
          ? parsed.mode
          : DEFAULT_STATE.mode,
      density:
        parsed.density === "compact" ||
        parsed.density === "comfortable" ||
        parsed.density === "spacious"
          ? parsed.density
          : DEFAULT_STATE.density,
      accent:
        typeof parsed.accent === "string" && parsed.accent in ACCENTS
          ? (parsed.accent as AccentKey)
          : DEFAULT_STATE.accent,
    };
  } catch {
    return DEFAULT_STATE;
  }
}

function writeState(s: TweaksState): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(s));
  } catch {
    // ignore quota / disabled storage
  }
}

function applyTweaks(s: TweaksState): void {
  if (typeof document === "undefined") return;
  const root = document.documentElement;
  const mode =
    s.mode === "system"
      ? window.matchMedia?.("(prefers-color-scheme: light)").matches
        ? "light"
        : "dark"
      : s.mode;
  root.setAttribute("data-theme", mode);
  if (s.density === "spacious") {
    root.removeAttribute("data-density");
    root.style.setProperty("--row-h", DENSITY_ROW_H.spacious);
  } else {
    root.setAttribute("data-density", s.density);
    root.style.removeProperty("--row-h");
  }
  root.style.setProperty("--accent-h", String(ACCENTS[s.accent].h));
}

export function installTweaks(): void {
  applyTweaks(readState());
}

export interface TweaksPanelProps {
  readonly open: boolean;
  readonly onClose: () => void;
}

export function TweaksPanel({ open, onClose }: TweaksPanelProps) {
  const [state, setState] = useState<TweaksState>(() => readState());
  const layout = useLayout();

  useEffect(() => {
    applyTweaks(state);
    writeState(state);
  }, [state]);

  if (!open) return null;

  const set = <K extends keyof TweaksState>(key: K, value: TweaksState[K]) =>
    setState((prev) => ({ ...prev, [key]: value }));

  return (
    <div className="tweaks show" role="dialog" aria-label="Appearance tweaks">
      <div className="t-header">
        <Sparkles style={{ width: 12, height: 12, color: "var(--accent)" }} />
        <span>Tweaks</span>
        <span style={{ flex: 1 }} />
        <button
          type="button"
          className="opacity-60 hover:opacity-100"
          aria-label="Close"
          onClick={onClose}
          style={{ color: "var(--fg-2)" }}
        >
          <XIcon style={{ width: 11, height: 11 }} />
        </button>
      </div>
      <div className="t-body">
        <div className="t-row">
          <span className="tk">Theme</span>
          <Seg
            value={state.mode}
            onChange={(v) => set("mode", v)}
            options={[
              { value: "dark", label: "Dark" },
              { value: "light", label: "Light" },
              { value: "system", label: "Sync" },
            ]}
          />
        </div>
        <div className="t-row">
          <span className="tk">Density</span>
          <Seg
            value={state.density}
            onChange={(v) => set("density", v)}
            options={[
              { value: "compact", label: "Compact" },
              { value: "comfortable", label: "Cosy" },
              { value: "spacious", label: "Spacious" },
            ]}
          />
        </div>
        <div className="t-sep" />
        <div className="t-row">
          <span className="tk">Show file tree</span>
          <button
            type="button"
            className={layout.fileTreeVisible ? "toggle on" : "toggle"}
            aria-label="Toggle file tree"
            onClick={() => layout.toggleFileTree()}
          />
        </div>
        <div className="t-row">
          <span className="tk">Show run drawer</span>
          <button
            type="button"
            className={layout.runDrawerVisible ? "toggle on" : "toggle"}
            aria-label="Toggle run drawer"
            onClick={() => layout.toggleRunDrawer()}
          />
        </div>
        <div className="t-sep" />
        <div className="t-row">
          <span className="tk">Accent</span>
          <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
            {(Object.keys(ACCENTS) as AccentKey[]).map((k) => (
              <button
                key={k}
                type="button"
                title={ACCENTS[k].name}
                onClick={() => set("accent", k)}
                className={
                  state.accent === k ? "accent-swatch active" : "accent-swatch"
                }
                style={{
                  background: `oklch(0.72 0.15 ${ACCENTS[k].h})`,
                }}
              />
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}

interface SegOption<V extends string> {
  readonly value: V;
  readonly label: string;
}

function Seg<V extends string>({
  value,
  onChange,
  options,
}: {
  readonly value: V;
  readonly onChange: (next: V) => void;
  readonly options: readonly SegOption<V>[];
}) {
  return (
    <div className="seg">
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          className={value === o.value ? "seg-btn active" : "seg-btn"}
          onClick={() => onChange(o.value)}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}
