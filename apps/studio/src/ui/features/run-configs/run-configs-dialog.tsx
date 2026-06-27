import { useEffect, useState } from "react";
import { Plus, Trash2, Copy, X } from "lucide-react";
import { useRunConfigs } from "../../../state/features/run-configs/index.js";
import type { RunConfig } from "../../../domain/features/run-configs/index.js";

export interface RunConfigsDialogProps {
  readonly open: boolean;
  readonly onClose: () => void;
}

/**
 * Create / edit / delete saved run configurations for the active
 * workspace. CRUD flows through the run-configs feature; this component
 * owns only ephemeral form state. Each saved config is persisted
 * into `<workspace>/.atomyx/run-configs.json`.
 */
export function RunConfigsDialog({ open, onClose }: RunConfigsDialogProps) {
  const { configs, save, remove, duplicate, activeId, setActive } =
    useRunConfigs();
  const [selectedId, setSelectedId] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    setSelectedId(activeId ?? configs[0]?.id ?? null);
  }, [open, activeId, configs]);

  if (!open) return null;

  const selected = configs.find((c) => c.id === selectedId) ?? null;

  async function handleNew() {
    const created = await save({
      name: `Run ${configs.length + 1}`,
    });
    setSelectedId(created.id);
  }

  async function handleDuplicate() {
    if (!selected) return;
    const copy = await duplicate(selected.id);
    if (copy) setSelectedId(copy.id);
  }

  async function handleDelete() {
    if (!selected) return;
    await remove(selected.id);
    setSelectedId(null);
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center"
      style={{ background: "rgba(0,0,0,0.45)" }}
      onClick={onClose}
    >
      <div
        className="w-[880px] h-[560px] rounded-md shadow-xl flex flex-col overflow-hidden"
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
            background: "var(--bg-2)",
            borderColor: "var(--line)",
          }}
        >
          <span className="text-sm font-semibold">Run Configurations</span>
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
            className="w-56 overflow-y-auto border-r flex flex-col"
            style={{
              background: "var(--bg-2)",
              borderColor: "var(--line)",
            }}
          >
            <div className="flex items-center gap-1 p-2">
              <button
                type="button"
                onClick={handleNew}
                className="p-1.5 rounded hover:bg-[var(--bg-hover)]"
                title="New"
              >
                <Plus className="h-3.5 w-3.5" />
              </button>
              <button
                type="button"
                onClick={handleDuplicate}
                disabled={!selected}
                className="p-1.5 rounded hover:bg-[var(--bg-hover)] disabled:opacity-40"
                title="Duplicate"
              >
                <Copy className="h-3.5 w-3.5" />
              </button>
              <button
                type="button"
                onClick={handleDelete}
                disabled={!selected}
                className="p-1.5 rounded hover:bg-[var(--bg-hover)] disabled:opacity-40"
                title="Delete"
                style={{ color: "var(--err)" }}
              >
                <Trash2 className="h-3.5 w-3.5" />
              </button>
            </div>
            <div className="flex-1 min-h-0 overflow-y-auto text-sm">
              {configs.length === 0 ? (
                <div
                  className="px-3 py-2 text-xs"
                  style={{ color: "var(--fg-2)" }}
                >
                  No configurations. Use the + button to add one.
                </div>
              ) : (
                configs.map((c) => {
                  const active = c.id === selectedId;
                  return (
                    <button
                      key={c.id}
                      type="button"
                      onClick={() => setSelectedId(c.id)}
                      className="w-full text-left px-3 py-1.5"
                      style={{
                        background: active
                          ? "var(--bg-hover)"
                          : "transparent",
                      }}
                    >
                      <div className="truncate">{c.name}</div>
                      <div
                        className="text-xs truncate"
                        style={{ color: "var(--fg-2)" }}
                      >
                        {c.deviceId ?? "no device"} ·{" "}
                        {c.appId ?? "no app"}
                      </div>
                    </button>
                  );
                })
              )}
            </div>
          </aside>
          <section className="flex-1 min-w-0 overflow-y-auto p-4">
            {selected ? (
              <ConfigForm
                config={selected}
                onSave={(patch) => void save({ ...patch, id: selected.id })}
                isActive={activeId === selected.id}
                onMakeActive={() => setActive(selected.id)}
              />
            ) : (
              <p
                className="text-sm"
                style={{ color: "var(--fg-2)" }}
              >
                Pick a configuration to edit, or add one with the + button.
              </p>
            )}
          </section>
        </div>
        <footer
          className="px-4 py-2 border-t flex justify-end gap-2 text-xs"
          style={{
            background: "var(--bg-2)",
            borderColor: "var(--line)",
          }}
        >
          <button
            type="button"
            onClick={onClose}
            className="px-3 py-1 rounded hover:bg-[var(--bg-hover)]"
          >
            Done
          </button>
        </footer>
      </div>
    </div>
  );
}

function ConfigForm({
  config,
  onSave,
  isActive,
  onMakeActive,
}: {
  readonly config: RunConfig;
  readonly onSave: (patch: { readonly name: string } & Partial<RunConfig>) => void;
  readonly isActive: boolean;
  readonly onMakeActive: () => void;
}) {
  return (
    <form
      className="space-y-4"
      onSubmit={(e) => e.preventDefault()}
    >
      <Field label="Name">
        <input
          value={config.name}
          onChange={(e) => onSave({ name: e.target.value })}
          className="w-full rounded px-2 py-1 text-sm"
          style={inputStyle}
        />
      </Field>
      <Field label="Device id">
        <input
          value={config.deviceId ?? ""}
          onChange={(e) =>
            onSave({ name: config.name, deviceId: e.target.value || null })
          }
          placeholder="UDID / serial, leave blank to pick at run time"
          className="w-full rounded px-2 py-1 text-sm"
          style={inputStyle}
        />
      </Field>
      <Field label="App id">
        <input
          value={config.appId ?? ""}
          onChange={(e) =>
            onSave({ name: config.name, appId: e.target.value || null })
          }
          placeholder="com.example.app"
          className="w-full rounded px-2 py-1 text-sm"
          style={inputStyle}
        />
      </Field>
      <Field label="Script (workspace-relative path)">
        <input
          value={config.scriptPath ?? ""}
          onChange={(e) =>
            onSave({
              name: config.name,
              scriptPath: e.target.value || null,
            })
          }
          placeholder="flows/login.yml — blank = active editor tab"
          className="w-full rounded px-2 py-1 text-sm"
          style={inputStyle}
        />
      </Field>
      <Field label="Env overrides (KEY=VALUE per line)">
        <textarea
          value={Object.entries(config.env)
            .map(([k, v]) => `${k}=${v}`)
            .join("\n")}
          onChange={(e) => {
            const next: Record<string, string> = {};
            for (const line of e.target.value.split("\n")) {
              const idx = line.indexOf("=");
              if (idx <= 0) continue;
              const key = line.slice(0, idx).trim();
              const value = line.slice(idx + 1);
              if (key) next[key] = value;
            }
            onSave({ name: config.name, env: next });
          }}
          rows={4}
          className="w-full rounded px-2 py-1 text-sm font-mono"
          style={inputStyle}
        />
      </Field>
      <div className="flex justify-between pt-2">
        <div
          className="text-xs"
          style={{ color: "var(--fg-2)" }}
        >
          Auto-saves as you type.
        </div>
        <button
          type="button"
          onClick={onMakeActive}
          disabled={isActive}
          className="px-3 py-1 rounded text-xs disabled:opacity-40"
          style={{
            background: isActive ? "transparent" : "var(--accent)",
            color: isActive
              ? "var(--fg-2)"
              : "var(--bg-1)",
          }}
        >
          {isActive ? "Already active" : "Make active"}
        </button>
      </div>
    </form>
  );
}

function Field({
  label,
  children,
}: {
  readonly label: string;
  readonly children: React.ReactNode;
}) {
  return (
    <label className="block space-y-1">
      <span
        className="text-[11px] uppercase tracking-wider block"
        style={{ color: "var(--fg-2)" }}
      >
        {label}
      </span>
      {children}
    </label>
  );
}

const inputStyle: React.CSSProperties = {
  background: "var(--bg-hover)",
  color: "var(--fg-0)",
  border: "1px solid var(--line)",
};
