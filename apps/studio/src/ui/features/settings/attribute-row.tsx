import { useMemo, useState } from "react";
import { useThemes } from "../../../state/features/theme/index.js";
import type {
  AttributeBundle,
  AttributeKey,
  FontStyle,
  Theme,
} from "../../../domain/features/theme/index.js";
import { FONT_STYLES } from "../../../domain/features/theme/index.js";
import { resolveInheritance, sourceForChannel } from "./inherited-from.js";

export interface AttributeRowProps {
  readonly attributeKey: AttributeKey;
}

function buildChain(
  library: ReadonlyMap<string, Theme>,
  activeId: string | null,
): readonly Theme[] {
  if (!activeId) return [];
  const out: Theme[] = [];
  const seen = new Set<string>();
  let cursor: Theme | undefined = library.get(activeId);
  let depth = 0;
  while (cursor && !seen.has(cursor.id) && depth < 8) {
    seen.add(cursor.id);
    out.push(cursor);
    if (!cursor.extends) break;
    cursor = library.get(cursor.extends);
    depth += 1;
  }
  return out;
}

export function AttributeRow({ attributeKey }: AttributeRowProps) {
  const {
    effective,
    library,
    activeId,
    overrides,
    setOverride,
  } = useThemes();
  const [localFg, setLocalFg] = useState<string | null>(null);
  const [localBg, setLocalBg] = useState<string | null>(null);

  const chain = useMemo(() => buildChain(library, activeId), [library, activeId]);
  const layers = resolveInheritance(attributeKey, chain, overrides);
  const override = overrides[attributeKey];
  const modified = override !== undefined;
  const bundle = effective[attributeKey];

  const fgSource = sourceForChannel(layers, "foreground");
  const bgSource = sourceForChannel(layers, "background");
  const fsSource = sourceForChannel(layers, "fontStyle");

  const inheritLabel = (
    source: ReturnType<typeof sourceForChannel>,
  ): string => {
    if (!source) return "default";
    const s = source.source;
    if (s.kind === "override") return "override";
    if (s.kind === "theme") return s.label;
    return "default";
  };

  async function writeOverride(patch: Partial<AttributeBundle>) {
    const next: AttributeBundle = { ...(override ?? {}), ...patch };
    const cleaned = stripUndefined(next as Record<string, unknown>);
    await setOverride(
      attributeKey,
      Object.keys(cleaned).length === 0 ? undefined : (cleaned as never),
    );
  }

  async function resetAttribute() {
    await setOverride(attributeKey, undefined);
  }

  return (
    <div
      className="flex items-center gap-3 px-4 py-2 border-b text-xs"
      style={{ borderColor: "var(--line)" }}
    >
      <div className="w-2 flex-shrink-0">
        {modified ? (
          <span
            title="Modified — click Reset to restore inherited value"
            style={{ color: "var(--accent)" }}
          >
            ●
          </span>
        ) : null}
      </div>
      <div className="w-56 flex-shrink-0 font-mono">{attributeKey}</div>

      <label
        className="flex items-center gap-1"
        title={`Foreground inherits from: ${inheritLabel(fgSource)} — ${bundle.foreground}`}
      >
        <span style={{ color: "var(--fg-2)" }}>fg</span>
        <input
          type="color"
          className="h-6 w-10 cursor-pointer bg-transparent"
          value={localFg ?? bundle.foreground}
          onChange={(e) => {
            setLocalFg(e.target.value);
            void writeOverride({ foreground: e.target.value });
          }}
        />
      </label>

      <label
        className="flex items-center gap-1"
        title={`Background inherits from: ${inheritLabel(bgSource)}`}
      >
        <span style={{ color: "var(--fg-2)" }}>bg</span>
        <input
          type="color"
          className="h-6 w-10 cursor-pointer bg-transparent"
          value={localBg ?? bundle.background ?? "#000000"}
          onChange={(e) => {
            setLocalBg(e.target.value);
            void writeOverride({ background: e.target.value });
          }}
        />
        {bundle.background === undefined && override?.background === undefined ? (
          <span
            className="text-[10px]"
            style={{ color: "var(--fg-2)" }}
          >
            unset
          </span>
        ) : null}
      </label>

      <label
        className="flex items-center gap-1"
        title={`Font style inherits from: ${inheritLabel(fsSource)}`}
      >
        <span style={{ color: "var(--fg-2)" }}>font</span>
        <select
          value={bundle.fontStyle ?? "normal"}
          onChange={(e) => {
            void writeOverride({
              fontStyle: e.target.value as FontStyle,
            });
          }}
          className="rounded px-1 py-0.5 text-[11px]"
          style={{
            background: "var(--bg-3)",
            color: "var(--fg-0)",
            border: "1px solid var(--line)",
          }}
        >
          {FONT_STYLES.map((fs) => (
            <option key={fs} value={fs}>
              {fs}
            </option>
          ))}
        </select>
      </label>

      <div className="flex-1" />
      <button
        type="button"
        onClick={() => void resetAttribute()}
        disabled={!modified}
        className="px-2 py-1 rounded disabled:opacity-30"
        style={{
          color: modified ? "var(--fg-1)" : "var(--fg-2)",
          border: "1px solid var(--line)",
        }}
        title="Reset to inherited value"
      >
        Reset
      </button>
    </div>
  );
}

function stripUndefined<T extends Record<string, unknown>>(o: T): Partial<T> {
  const out: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(o)) {
    if (v !== undefined) out[k] = v;
  }
  return out as Partial<T>;
}
