import { useMemo, useState } from "react";
import { X, RotateCcw, FolderOpen, Palette } from "lucide-react";
import { Button } from "../../primitives/button.js";
import { useSettings } from "../../../state/features/settings/index.js";
import { useThemes } from "../../../state/features/theme/index.js";
import { SETTINGS_CATEGORIES, type SettingsCategory } from "./category.js";
import { AttributeRow } from "./attribute-row.js";

export interface SettingsDialogProps {
  readonly open: boolean;
  readonly onClose: () => void;
}

/**
 * IntelliJ-style Preferences modal. Tree of categories on the
 * left, attribute list for the selected category on the right,
 * with per-attribute overrides that flow directly into the
 * theme store.
 *
 * Keyboard: ⌘, opens (wired in app-shell), Esc closes.
 */
export function SettingsDialog({ open, onClose }: SettingsDialogProps) {
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

  if (!open) return null;

  async function onReload() {
    await openThemesDir().catch(() => {});
    await reload();
  }

  async function onOpenFolder() {
    await openThemesDir();
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center"
      style={{ background: "rgba(0,0,0,0.45)" }}
      onClick={onClose}
    >
      <div
        className="w-[900px] h-[620px] rounded-md shadow-xl flex flex-col overflow-hidden"
        style={{
          background: "var(--bg-1)",
          color: "var(--fg-0)",
          border: "1px solid var(--line)",
        }}
        onClick={(e) => e.stopPropagation()}
      >
        <header
          className="flex items-center justify-between px-4 py-2 border-b"
          style={{
            background: "var(--bg-3)",
            borderColor: "var(--line)",
          }}
        >
          <div className="flex items-center gap-2">
            <Palette className="h-4 w-4" style={{ color: "var(--accent)" }} />
            <span className="text-sm font-semibold">Preferences</span>
            <span
              className="text-xs"
              style={{ color: "var(--fg-2)" }}
            >
              Editor · Color Scheme
            </span>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="opacity-60 hover:opacity-100"
            aria-label="Close"
          >
            <X className="h-4 w-4" />
          </button>
        </header>

        <div className="flex-1 min-h-0 flex">
          <aside
            className="w-56 overflow-y-auto border-r"
            style={{
              background: "var(--bg-3)",
              borderColor: "var(--line)",
            }}
          >
            <div className="p-3 space-y-3">
              <div>
                <label
                  className="text-[11px] uppercase tracking-wider block mb-1"
                  style={{ color: "var(--fg-2)" }}
                >
                  Scheme
                </label>
                <select
                  value={activeId ?? ""}
                  onChange={(e) => void setActiveId(e.target.value)}
                  className="w-full rounded px-2 py-1 text-xs"
                  style={{
                    background: "var(--bg-hover)",
                    color: "var(--fg-0)",
                    border: "1px solid var(--line)",
                  }}
                >
                  {available.map((t) => (
                    <option key={t.id} value={t.id}>
                      {t.label}
                      {t.source === "user" ? " (user)" : ""}
                    </option>
                  ))}
                </select>
              </div>
              <div className="space-y-1">
                {SETTINGS_CATEGORIES.map((c) => {
                  const active = c.id === activeCategory.id;
                  return (
                    <button
                      key={c.id}
                      type="button"
                      onClick={() => setActiveCategoryId(c.id)}
                      className="w-full text-left px-2 py-1 rounded text-xs"
                      style={{
                        background: active
                          ? "var(--bg-hover)"
                          : "transparent",
                        color: active
                          ? "var(--fg-0)"
                          : "var(--fg-1)",
                      }}
                    >
                      {c.label}
                    </button>
                  );
                })}
              </div>
            </div>
          </aside>

          <section className="flex-1 min-w-0 flex flex-col">
            <div
              className="px-4 py-3 border-b space-y-2"
              style={{ borderColor: "var(--line)" }}
            >
              <div className="flex items-center justify-between gap-3">
                <h2 className="text-sm font-semibold">
                  {activeCategory.label}
                </h2>
                <input
                  value={filter}
                  onChange={(e) => setFilter(e.target.value)}
                  placeholder="Filter attributes…"
                  className="w-48 rounded px-2 py-1 text-xs"
                  style={{
                    background: "var(--bg-hover)",
                    color: "var(--fg-0)",
                    border: "1px solid var(--line)",
                  }}
                />
              </div>
              <p
                className="text-xs"
                style={{ color: "var(--fg-2)" }}
              >
                {activeCategory.description}
              </p>
            </div>
            <div className="flex-1 min-h-0 overflow-y-auto">
              {visibleKeys.length === 0 ? (
                <div
                  className="px-4 py-2 text-xs"
                  style={{ color: "var(--fg-2)" }}
                >
                  No attributes match &quot;{filter}&quot;.
                </div>
              ) : (
                visibleKeys.map((key) => (
                  <AttributeRow key={key} attributeKey={key} />
                ))
              )}
            </div>
          </section>
        </div>

        <footer
          className="px-4 py-2 border-t flex items-center justify-between text-xs"
          style={{
            background: "var(--bg-3)",
            borderColor: "var(--line)",
          }}
        >
          <div
            className="flex items-center gap-4"
            style={{ color: "var(--fg-2)" }}
          >
            <span>
              {overrideCount > 0
                ? `${overrideCount} attribute${overrideCount === 1 ? "" : "s"} overridden`
                : "No overrides"}
            </span>
            <label className="flex items-center gap-1">
              <input
                type="checkbox"
                checked={settings.useBundledFont}
                onChange={(e) =>
                  void update({ useBundledFont: e.target.checked })
                }
              />
              JetBrains Mono
            </label>
            <label className="flex items-center gap-1">
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
            <label className="flex items-center gap-1">
              <input
                type="checkbox"
                checked={settings.autoSaveOnBlur}
                onChange={(e) =>
                  void update({ autoSaveOnBlur: e.target.checked })
                }
              />
              Auto-save on blur
            </label>
          </div>
          <div className="flex items-center gap-2">
            <Button
              variant="ghost"
              size="sm"
              onClick={onOpenFolder}
              title="Open user themes folder"
            >
              <FolderOpen className="h-3 w-3 mr-1" /> Open folder
            </Button>
            <Button variant="ghost" size="sm" onClick={onReload}>
              <RotateCcw className="h-3 w-3 mr-1" /> Reload
            </Button>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => void clearOverrides()}
              disabled={overrideCount === 0}
            >
              Reset overrides
            </Button>
            <Button variant="primary" size="sm" onClick={onClose}>
              Done
            </Button>
          </div>
        </footer>
      </div>
    </div>
  );
}
