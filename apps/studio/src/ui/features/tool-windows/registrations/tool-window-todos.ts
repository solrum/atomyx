import { createElement } from "react";
import { CheckSquare } from "lucide-react";
import { getFeature } from "../../../../state/core/registry.js";
import { toolWindowRegistry } from "../../../shell/tool-window-registry.js";
import type { LayoutApi } from "../../../../state/features/layout/index.js";
import { LAYOUT_KEY } from "../../../../state/features/layout/index.js";
import type { TodosApi } from "../../../../state/features/todos/index.js";
import { TODOS_KEY } from "../../../../state/features/todos/index.js";
import { TodosList } from "../todos-list.js";

function TodosListAdapter() {
  return createElement(TodosList, { loading: getFeature<TodosApi>(TODOS_KEY).getSnapshot().loading });
}

toolWindowRegistry.register({
  id: "todos",
  side: "bottom",
  icon: createElement(CheckSquare, { className: "h-3.5 w-3.5" }),
  label: "TODO",
  isVisible: () => {
    const s = getFeature<LayoutApi>(LAYOUT_KEY).getSnapshot();
    return s.problemsVisible && s.bottomPane === "todos";
  },
  toggle: () => {
    getFeature<LayoutApi>(LAYOUT_KEY).toggleTodos();
    void getFeature<TodosApi>(TODOS_KEY).refresh();
  },
  body: TodosListAdapter,
  badge: () => {
    const n = getFeature<TodosApi>(TODOS_KEY).getSnapshot().items.length;
    return n > 0 ? n : null;
  },
});
