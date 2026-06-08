import { AlertTriangle, AlertCircle, Info } from "lucide-react";
import { useProblems, type Problem } from "../../../state/features/problems/index.js";
import { jumpActiveEditorTo } from "../editor/index.js";

export function ProblemsList() {
  const { items } = useProblems();
  if (items.length === 0) {
    return (
      <div
        className="px-3 py-2 text-xs"
        style={{ color: "var(--fg-2)" }}
      >
        No problems detected in the active file.
      </div>
    );
  }
  return (
    <>
      {items.map((p, i) => (
        <ProblemRow key={`${i}-${p.line}-${p.column}`} item={p} />
      ))}
    </>
  );
}

function ProblemRow({ item }: { readonly item: Problem }) {
  const icon =
    item.severity === "error" ? (
      <AlertCircle
        className="h-3 w-3 flex-none"
        style={{ color: "var(--err)" }}
      />
    ) : item.severity === "warning" ? (
      <AlertTriangle
        className="h-3 w-3 flex-none"
        style={{ color: "var(--warn)" }}
      />
    ) : (
      <Info
        className="h-3 w-3 flex-none"
        style={{ color: "var(--fg-2)" }}
      />
    );
  return (
    <button
      type="button"
      onClick={() => jumpActiveEditorTo(item.line, item.column)}
      className="w-full flex items-start gap-2 px-3 py-1 text-left text-xs hover:bg-[color:var(--bg-hover)]"
    >
      {icon}
      <span className="flex-1 min-w-0">
        <span className="truncate">{item.message}</span>
        {item.source ? (
          <span
            className="ml-2 text-[10px]"
            style={{ color: "var(--fg-2)" }}
          >
            {item.source}
          </span>
        ) : null}
      </span>
      <span
        className="text-[10px] font-mono flex-none"
        style={{ color: "var(--fg-2)" }}
      >
        {item.line}:{item.column}
      </span>
    </button>
  );
}
