import { useMemo, useState } from "react";
import { X, RotateCcw, FolderOpen } from "lucide-react";
import { useSettings } from "../../../state/features/settings/index.js";
import { useThemes } from "../../../state/features/theme/index.js";
import { useLayout } from "../../../state/features/layout/index.js";
import { SETTINGS_CATEGORIES, type SettingsCategory } from "./settings-category.js";
import { AttributeRow } from "./settings-attribute-row.js";

const NAV_GROUPS: readonly {
  readonly id: string;
  readonly label: string;
  readonly categoryIds: readonly string[];
}[] = [
  {
    id: "runtime",
    label: "Runtime",
    categoryIds: ["run", "diagnostics", "mcp", "devices", "inspector", "artifacts"],
  },
  {
    id: "system",
    label: "System",
    categoryIds: ["updates", "telemetry", "about"],
  },
];

/**
 * Full-page preferences view. Replaces the work area while
 * `layout.settingsViewVisible` is true. Layout matches the design:
 * 200px nav on the left + 1fr body, no chrome bar at the top.
 */
export function SettingsView() {
  const [activeCategoryId, setActiveCategoryId] = useState<string>(
    SETTINGS_CATEGORIES[0]!.id,
  );
  const [filter, setFilter] = useState("");
  const { settings, update } = useSettings();
  const {
    available,
    activeId,
    setActiveId,
    reload,
    overrides,
    clearOverrides,
    openThemesDir,
  } = useThemes();
  const layout = useLayout();

  const activeCategory = useMemo<SettingsCategory>(
    () =>
      SETTINGS_CATEGORIES.find((c) => c.id === activeCategoryId) ??
      SETTINGS_CATEGORIES[0]!,
    [activeCategoryId],
  );

  const visibleKeys = useMemo(() => {
    const q = filter.trim().toLowerCase();
    if (q.length === 0) return activeCategory.keys;
    return activeCategory.keys.filter((k) => k.toLowerCase().includes(q));
  }, [activeCategory, filter]);

  const overrideCount = Object.keys(overrides).length;

  async function onReload() {
    await openThemesDir().catch(() => {});
    await reload();
  }

  const onClose = () => layout.setSettingsView(false);

  return (
    <div className="settings h-full">
      <div className="settings-nav">
        <div className="group-title">Theme</div>
        <select
          value={activeId ?? ""}
          onChange={(e) => void setActiveId(e.target.value)}
          className="select"
          style={{ margin: "0 14px 12px", width: "calc(100% - 28px)" }}
        >
          {available.map((t) => (
            <option key={t.id} value={t.id}>
              {t.label}
              {t.source === "user" ? " (user)" : ""}
            </option>
          ))}
        </select>
        {NAV_GROUPS.map((group) => {
          const items = SETTINGS_CATEGORIES.filter((c) =>
            group.categoryIds.includes(c.id),
          );
          if (items.length === 0) return null;
          return (
            <div key={group.id}>
              <div className="group-title">{group.label}</div>
              {items.map((c) => (
                <button
                  key={c.id}
                  type="button"
                  onClick={() => setActiveCategoryId(c.id)}
                  className={
                    c.id === activeCategory.id
                      ? "nav-item active"
                      : "nav-item"
                  }
                >
                  {c.label}
                </button>
              ))}
            </div>
          );
        })}
      </div>

      <section className="settings-body" style={{ position: "relative" }}>
        <button
          type="button"
          onClick={onClose}
          aria-label="Close settings"
          className="opacity-60 hover:opacity-100"
          style={{
            position: "absolute",
            top: "var(--gap-4)",
            right: "var(--gap-5)",
            color: "var(--fg-2)",
          }}
        >
          <X style={{ width: 14, height: 14 }} />
        </button>

        <h2>{activeCategory.label}</h2>
        <div className="sub">{activeCategory.description}</div>

        {activeCategory.id === "inspector" ? (
          <InspectorAutoRefreshRow
            enabled={settings.inspectorAutoRefresh.enabled}
            intervalMs={settings.inspectorAutoRefresh.intervalMs}
            onChange={(next) =>
              void update({ inspectorAutoRefresh: next })
            }
          />
        ) : (
          <>
            <div className="setting-row">
              <div>
                <div className="sk">Filter attributes</div>
                <div className="sd">
                  Narrow the list to attributes whose key matches.
                </div>
              </div>
              <div className="sv">
                <input
                  className="input"
                  value={filter}
                  onChange={(e) => setFilter(e.target.value)}
                  placeholder="Type to filter…"
                  style={{ maxWidth: 320 }}
                />
              </div>
            </div>

            {visibleKeys.length === 0 ? (
              <div
                style={{
                  padding: "var(--gap-4) 0",
                  fontSize: "var(--fs-12)",
                  color: "var(--fg-2)",
                }}
              >
                No attributes match &quot;{filter}&quot;.
              </div>
            ) : (
              visibleKeys.map((key) => (
                <AttributeRow key={key} attributeKey={key} />
              ))
            )}
          </>
        )}

        <div
          className="flex items-center"
          style={{
            gap: "var(--gap-5)",
            marginTop: "var(--gap-7)",
            paddingTop: "var(--gap-5)",
            borderTop: "1px solid var(--line-soft)",
            fontSize: "var(--fs-12)",
            color: "var(--fg-2)",
          }}
        >
          <span>
            {overrideCount > 0
              ? `${overrideCount} attribute${overrideCount === 1 ? "" : "s"} overridden`
              : "No overrides"}
          </span>
          <label className="flex items-center" style={{ gap: "var(--gap-2)" }}>
            <input
              type="checkbox"
              checked={settings.useBundledFont}
              onChange={(e) =>
                void update({ useBundledFont: e.target.checked })
              }
            />
            JetBrains Mono
          </label>
          <label className="flex items-center" style={{ gap: "var(--gap-2)" }}>
            <input
              type="checkbox"
              checked={settings.stripTrailingWhitespaceOnSave}
              onChange={(e) =>
                void update({
                  stripTrailingWhitespaceOnSave: e.target.checked,
                })
              }
            />
            Strip trailing ws
          </label>
          <label className="flex items-center" style={{ gap: "var(--gap-2)" }}>
            <input
              type="checkbox"
              checked={settings.autoSaveOnBlur}
              onChange={(e) =>
                void update({ autoSaveOnBlur: e.target.checked })
              }
            />
            Auto-save on blur
          </label>
          <div style={{ flex: 1 }} />
          <button
            type="button"
            className="btn ghost"
            onClick={() => void openThemesDir()}
            title="Open user themes folder"
          >
            <FolderOpen style={{ width: 12, height: 12 }} /> Open folder
          </button>
          <button type="button" className="btn ghost" onClick={onReload}>
            <RotateCcw style={{ width: 12, height: 12 }} /> Reload
          </button>
          <button
            type="button"
            className="btn"
            onClick={() => void clearOverrides()}
            disabled={overrideCount === 0}
          >
            Reset overrides
          </button>
          <button type="button" className="btn primary" onClick={onClose}>
            Done
          </button>
        </div>
      </section>
    </div>
  );
}

const INSPECTOR_INTERVAL_OPTIONS: readonly { ms: number; label: string }[] = [
  { ms: 2000, label: "2 seconds" },
  { ms: 5000, label: "5 seconds" },
  { ms: 10_000, label: "10 seconds" },
  { ms: 30_000, label: "30 seconds" },
];

interface InspectorAutoRefreshRowProps {
  readonly enabled: boolean;
  readonly intervalMs: number;
  readonly onChange: (next: { enabled: boolean; intervalMs: number }) => void;
}

function InspectorAutoRefreshRow({
  enabled,
  intervalMs,
  onChange,
}: InspectorAutoRefreshRowProps) {
  return (
    <>
      <div className="setting-row">
        <div>
          <div className="sk">Auto-refresh UI tree</div>
          <div className="sd">
            Polls the inspector for the bound device. Disabled by default;
            ticks pause briefly after each tap or swipe so the captured tree
            reflects the post-interaction state.
          </div>
        </div>
        <div className="sv">
          <label className="flex items-center" style={{ gap: "var(--gap-2)" }}>
            <input
              type="checkbox"
              checked={enabled}
              onChange={(e) =>
                onChange({ enabled: e.target.checked, intervalMs })
              }
            />
            Enable
          </label>
        </div>
      </div>
      <div className="setting-row">
        <div>
          <div className="sk">Refresh interval</div>
          <div className="sd">
            Lower values keep the tree fresher but issue more dump calls;
            iOS XCUITest dumps can take ~1 second so values below 2s are
            ignored.
          </div>
        </div>
        <div className="sv">
          <select
            className="select"
            value={intervalMs}
            disabled={!enabled}
            onChange={(e) =>
              onChange({ enabled, intervalMs: Number(e.target.value) })
            }
            style={{ maxWidth: 200 }}
          >
            {INSPECTOR_INTERVAL_OPTIONS.map((opt) => (
              <option key={opt.ms} value={opt.ms}>
                {opt.label}
              </option>
            ))}
          </select>
        </div>
      </div>
    </>
  );
}
