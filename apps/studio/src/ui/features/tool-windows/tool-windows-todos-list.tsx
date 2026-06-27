import { getFeature } from "../../../state/core/registry.js";
import { useTodos } from "../../../state/features/todos/index.js";
import type { TodoHit } from "../../../domain/features/todos/index.js";
import type { EditorApi } from "../../../state/features/editor/index.js";
import { EDITOR_KEY } from "../../../state/features/editor/index.js";
import { jumpActiveEditorTo } from "../editor/index.js";

export function TodosList({ loading }: { readonly loading: boolean }) {
  const { items } = useTodos();
  if (loading && items.length === 0) {
    return (
      <div
        className="px-3 py-2 text-xs"
        style={{ color: "var(--fg-2)" }}
      >
        Scanning…
      </div>
    );
  }
  if (items.length === 0) {
    return (
      <div
        className="px-3 py-2 text-xs"
        style={{ color: "var(--fg-2)" }}
      >
        No TODO / FIXME / HACK / XXX / NOTE markers found.
      </div>
    );
  }
  return (
    <>
      {items.map((t, i) => (
        <TodoRow key={`${i}-${t.path}-${t.line}`} item={t} />
      ))}
    </>
  );
}

function TodoRow({ item }: { readonly item: TodoHit }) {
  const open = async () => {
    await getFeature<EditorApi>(EDITOR_KEY).openFile(item.path);
    requestAnimationFrame(() => jumpActiveEditorTo(item.line, 1));
  };
  return (
    <button
      type="button"
      onClick={() => void open()}
      className="w-full flex items-start gap-2 px-3 py-1 text-left text-xs hover:bg-[var(--bg-hover)]"
    >
      <span
        className="flex-none font-mono text-[10px] px-1 rounded"
        style={{
          color: kindColor(item.kind),
          border: `1px solid ${kindColor(item.kind)}`,
        }}
      >
        {item.kind}
      </span>
      <span className="flex-1 min-w-0 truncate">{item.snippet}</span>
      <span
        className="text-[10px] flex-none"
        style={{ color: "var(--fg-2)" }}
      >
        {item.path.split("/").pop()}:{item.line}
      </span>
    </button>
  );
}

function kindColor(kind: string): string {
  switch (kind) {
    case "FIXME":
    case "XXX":
      return "var(--err)";
    case "HACK":
      return "var(--warn)";
    case "NOTE":
      return "var(--fg-2)";
    default:
      return "var(--accent)";
  }
}
