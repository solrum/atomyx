import { createStore } from "zustand/vanilla";
import {
  fillDefaults,
  mergeTheme,
  parseTheme,
  type ThemeStore as ThemePort,
} from "../../../domain/features/theme/index.js";
import type {
  EffectiveAttributes,
  Theme,
  ThemeApi,
  ThemeListEntry,
  ThemeOverrides,
  ThemeSnapshot,
} from "./theme.contract.js";

export interface ThemeDeps {
  readonly store: ThemePort;
}

const EMPTY_EFFECTIVE: EffectiveAttributes = fillDefaults({});

function computeEffective(
  library: ReadonlyMap<string, Theme>,
  activeId: string | null,
  overrides: ThemeOverrides,
): { effective: EffectiveAttributes; issues: string[] } {
  if (!activeId) return { effective: EMPTY_EFFECTIVE, issues: [] };
  const result = mergeTheme(activeId, library, overrides);
  if (result.ok) return { effective: result.attributes, issues: [] };
  return {
    effective: EMPTY_EFFECTIVE,
    issues: result.issues.map((i) => `${i.code}: ${i.message}`),
  };
}

export function createZustandTheme(deps: ThemeDeps): ThemeApi {
  const { store: port } = deps;
  const store = createStore<ThemeSnapshot>(() => ({
    available: [],
    library: new Map(),
    activeId: null,
    overrides: {},
    effective: EMPTY_EFFECTIVE,
    issues: [],
  }));

  const api: ThemeApi = {
    getSnapshot: () => store.getState(),
    subscribe: (listener) => store.subscribe(listener),

    async reload(workspacePath?: string) {
      const [builtIns, users, workspace] = await Promise.all([
        port.listBuiltIns(),
        port.listUser(),
        workspacePath ? port.listWorkspace(workspacePath) : Promise.resolve([]),
      ]);
      const raw = [...builtIns, ...users, ...workspace];

      const library = new Map<string, Theme>();
      const available: ThemeListEntry[] = [];
      const issues: string[] = [];
      for (const r of raw) {
        const parsed = parseTheme(r.json);
        if (!parsed.ok) {
          issues.push(
            `${r.source} theme ignored: ${parsed.issues
              .map((i) => `${i.path}: ${i.message}`)
              .join("; ")}`,
          );
          continue;
        }
        library.set(parsed.theme.id, parsed.theme);
        available.push({
          id: parsed.theme.id,
          label: parsed.theme.label,
          source: r.source,
        });
      }

      const current = store.getState();
      const activeId = current.activeId ?? available[0]?.id ?? null;
      const { effective, issues: mergeIssues } = computeEffective(
        library,
        activeId,
        current.overrides,
      );
      store.setState({
        available,
        library,
        activeId,
        effective,
        issues: [...issues, ...mergeIssues],
      });
    },

    async setActiveId(id) {
      let current = store.getState();
      if (!current.library.has(id)) {
        await api.reload();
        current = store.getState();
      }
      const { effective, issues } = computeEffective(
        current.library,
        id,
        current.overrides,
      );
      store.setState({ activeId: id, effective, issues });
    },

    async setOverride(key, bundle) {
      const current = store.getState();
      const next: ThemeOverrides = { ...current.overrides };
      if (bundle === undefined) {
        delete (next as Record<string, unknown>)[key];
      } else {
        (next as Record<string, unknown>)[key] = bundle;
      }
      const { effective, issues } = computeEffective(
        current.library,
        current.activeId,
        next,
      );
      store.setState({ overrides: next, effective, issues });
    },

    async clearOverrides() {
      const current = store.getState();
      const { effective, issues } = computeEffective(
        current.library,
        current.activeId,
        {},
      );
      store.setState({ overrides: {}, effective, issues });
    },

    async openThemesDir() {
      await port.openThemesDir();
    },
  };

  return api;
}
