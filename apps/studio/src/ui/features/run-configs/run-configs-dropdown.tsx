import { useState } from "react";
import { ChevronDown, Plus, Pencil } from "lucide-react";
import { useRunConfigs } from "../../../state/features/run-configs/index.js";
import { RunConfigsDialog } from "./run-configs-dialog.js";

/**
 * Toolbar dropdown that picks the active run configuration.
 * Clicking the main face exposes the list + an "Edit
 * configurations…" entry that opens the CRUD dialog. Behavior is
 * IntelliJ-inspired: one shot to switch, one click to manage.
 */
export function RunConfigsDropdown() {
  const { configs, activeId, setActive } = useRunConfigs();
  const [open, setOpen] = useState(false);
  const [dialogOpen, setDialogOpen] = useState(false);

  const active = configs.find((c) => c.id === activeId);
  const label = active ? active.name : "No configuration";

  return (
    <>
      <div className="relative">
        <button
          type="button"
          onClick={() => setOpen((o) => !o)}
          className="inline-flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-[13px] transition-colors hover:bg-[var(--bg-hover)]"
          style={{ color: "var(--fg-1)" }}
          title="Run configurations"
        >
          <span
            className="inline-block h-1.5 w-1.5 rounded-full"
            style={{
              background: active ? "var(--accent)" : "var(--fg-2)",
            }}
          />
          <span className="truncate max-w-[180px]">{label}</span>
          <ChevronDown className="h-3 w-3" />
        </button>
        {open ? (
          <div
            className="absolute left-0 top-full mt-1 min-w-[260px] rounded-md shadow-xl py-1 z-40 text-sm"
            style={{
              background: "var(--bg-2)",
              color: "var(--fg-0)",
              border: "1px solid var(--line)",
            }}
            onMouseLeave={() => setOpen(false)}
          >
            {configs.length === 0 ? (
              <div
                className="px-3 py-2 text-xs"
                style={{ color: "var(--fg-2)" }}
              >
                No saved configurations yet.
              </div>
            ) : (
              configs.map((c) => (
                <button
                  key={c.id}
                  type="button"
                  onClick={() => {
                    setActive(c.id);
                    setOpen(false);
                  }}
                  className="w-full flex items-center justify-between gap-2 px-3 py-1.5 hover:bg-[var(--bg-hover)]"
                >
                  <span className="truncate">{c.name}</span>
                  {c.id === activeId ? (
                    <span
                      className="text-[11px]"
                      style={{ color: "var(--accent)" }}
                    >
                      active
                    </span>
                  ) : null}
                </button>
              ))
            )}
            <div
              className="my-1 h-px"
              style={{ background: "var(--line)" }}
            />
            <button
              type="button"
              onClick={() => {
                setOpen(false);
                setDialogOpen(true);
              }}
              className="w-full flex items-center gap-2 px-3 py-1.5 hover:bg-[var(--bg-hover)]"
            >
              <Pencil className="h-3 w-3" />
              Edit configurations…
            </button>
            <button
              type="button"
              onClick={() => {
                setOpen(false);
                setDialogOpen(true);
              }}
              className="w-full flex items-center gap-2 px-3 py-1.5 hover:bg-[var(--bg-hover)]"
            >
              <Plus className="h-3 w-3" />
              New configuration…
            </button>
          </div>
        ) : null}
      </div>
      <RunConfigsDialog
        open={dialogOpen}
        onClose={() => setDialogOpen(false)}
      />
    </>
  );
}
