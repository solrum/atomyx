import { useEffect, createElement } from "react";
import { X, RotateCcw } from "lucide-react";
import { useLayout } from "../../../state/features/layout/index.js";
import { useTodos } from "../../../state/features/todos/index.js";
import { toolWindowRegistry } from "../../shell/tool-window-registry.js";

export function BottomToolWindow() {
  const layout = useLayout();
  const todosState = useTodos();

  const tabs = toolWindowRegistry.bySide("bottom");
  const active = tabs.find((d) => d.id === layout.bottomPane) ?? tabs[0];
  const activeBadge = active?.badge?.() ?? null;

  useEffect(() => {
    if (layout.bottomPane === "todos" && todosState.items.length === 0) {
      void todosState.refresh();
    }
  }, [layout.bottomPane, todosState.items.length, todosState.refresh]);

  const close = () => layout.setProblems(false);

  return (
    <section
      className="pane h-full"
      style={{ borderRight: 0, borderTop: "1px solid var(--line)" }}
    >
      <div
        className="flex items-center"
        style={{
          height: 28,
          padding: "0 var(--gap-4)",
          borderBottom: "1px solid var(--line)",
          background: "var(--bg-2)",
          fontSize: "10px",
          textTransform: "uppercase",
          letterSpacing: "0.06em",
          color: "var(--fg-2)",
          gap: "var(--gap-2)",
        }}
      >
        {active?.icon}
        <span>{active?.label}</span>
        {activeBadge !== null ? (
          <span className="rt-count">{activeBadge}</span>
        ) : null}
        <div style={{ flex: 1 }} />
        {layout.bottomPane === "todos" ? (
          <button
            type="button"
            aria-label="Refresh TODOs"
            onClick={() => void todosState.refresh()}
            className="opacity-60 hover:opacity-100"
            style={{ color: "var(--fg-2)" }}
            disabled={todosState.loading}
          >
            <RotateCcw className="h-3 w-3" />
          </button>
        ) : null}
        <button
          type="button"
          aria-label="Close"
          onClick={close}
          className="opacity-60 hover:opacity-100"
          style={{ color: "var(--fg-2)" }}
        >
          <X className="h-3 w-3" />
        </button>
      </div>
      <div className="pane-body">
        {active?.body ? createElement(active.body) : null}
      </div>
    </section>
  );
}
