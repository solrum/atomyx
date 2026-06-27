import { createStore } from "zustand/vanilla";
import { v4 as uuid } from "uuid";
import type {
  ArtifactStore,
  RunMetadata,
  RunResult,
} from "../../../domain/features/artifacts/index.js";
import type {
  RunEvent,
  StudioRuntime,
} from "../../../domain/features/runtime/index.js";
import type {
  LiveRun,
  RunsApi,
  RunsSnapshot,
  ScenarioLiveState,
  ScenarioScriptRow,
} from "./runs.contract.js";

export interface RunsDeps {
  readonly runtime: StudioRuntime;
  readonly artifacts: ArtifactStore;
}

export function createZustandRuns(deps: RunsDeps): RunsApi {
  const { runtime, artifacts } = deps;
  const store = createStore<RunsSnapshot>(() => ({
    live: null,
    history: [],
  }));
  let cancelled = false;

  const api: RunsApi = {
    getSnapshot: () => store.getState(),
    subscribe: (listener) => store.subscribe(listener),

    async startRun(scriptPath, yaml, opts) {
      cancelled = false;
      const runId = uuid();
      const startedAt = Date.now();

      const live: LiveRun = {
        runId,
        scriptPath,
        deviceId: opts.deviceId,
        startedAt,
        events: [],
        result: "running",
      };
      store.setState({ live });

      const scriptName = scriptPath.split("/").pop() ?? scriptPath;
      const meta: RunMetadata = {
        runId,
        scriptPath,
        scriptName,
        deviceId: opts.deviceId,
        startedAt,
      };
      await artifacts.createRun(meta);

      let failedAtStep: number | undefined;
      let okResult = true;
      let totalSteps = 0;

      // Resolve relative paths inside the script / scenario against
      // the directory holding it. Both `requires` and the scenario's
      // `scripts` list need this anchor; absolute paths in either
      // place still work because the loader resolves them as-is.
      const cwd = opts.cwd ?? scriptPath.replace(/\/[^/]*$/, "");
      const runOpts = { ...opts, cwd };

      try {
        for await (const event of runtime.runScript(yaml, runOpts)) {
          if (cancelled) break;
          await artifacts.appendEvent(runId, event);
          const updated = store.getState().live;
          if (updated && updated.runId === runId) {
            store.setState({
              live: applyEventToLiveRun(updated, event),
            });
          }
          if (event.type === "stepCompleted") {
            totalSteps = Math.max(totalSteps, event.stepIndex + 1);
            if (!event.ok) {
              failedAtStep = event.stepIndex;
            }
          }
          if (event.type === "scenarioCompleted") {
            okResult = event.ok;
            break;
          }
          if (event.type === "runCompleted") {
            okResult = event.ok;
            break;
          }
        }
      } catch (err) {
        okResult = false;
        await artifacts.appendEvent(runId, {
          type: "consoleLog",
          level: "error",
          line: err instanceof Error ? err.message : String(err),
          at: Date.now(),
        });
      }

      const result: RunResult = cancelled
        ? "cancelled"
        : okResult
          ? "passed"
          : "failed";

      await artifacts.finalizeRun(runId, {
        completedAt: Date.now(),
        result,
        failedAtStep,
        totalSteps,
      });

      const current = store.getState().live;
      if (current && current.runId === runId) {
        store.setState({ live: { ...current, result } });
      }
      await api.loadHistory();
    },

    stopRun() {
      cancelled = true;
      // Fire-and-forget: the runtime adapter signals the sidecar to
      // abort the in-flight run. The cancelled flag above ends the
      // event-drain loop in startRun once the iterator winds down.
      void runtime.stop().catch(() => {
        /* sidecar may already be idle; nothing to do */
      });
    },

    async loadHistory() {
      const history = await artifacts.listRuns();
      store.setState({ history });
    },

    async deleteRun(runId) {
      await artifacts.deleteRun(runId);
      await api.loadHistory();
    },
  };

  return api;
}

function applyEventToLiveRun(live: LiveRun, event: RunEvent): LiveRun {
  const events = [...live.events, event];
  if (event.type === "scenarioStarted") {
    const scenario: ScenarioLiveState = {
      name: event.scenarioName,
      totalScripts: event.totalScripts,
      currentIndex: -1,
      scripts: Array.from({ length: event.totalScripts }, (_, i) => ({
        index: i,
        path: "",
        status: "queued" as const,
      })),
    };
    return { ...live, events, scenario };
  }
  if (event.type === "scriptStarted" && live.scenario) {
    const scripts: ScenarioScriptRow[] = live.scenario.scripts.map((row) =>
      row.index === event.scriptIndex
        ? { ...row, path: event.scriptPath, status: "running" }
        : row,
    );
    return {
      ...live,
      events,
      scenario: {
        ...live.scenario,
        currentIndex: event.scriptIndex,
        scripts,
      },
    };
  }
  if (event.type === "scriptCompleted" && live.scenario) {
    const scripts: ScenarioScriptRow[] = live.scenario.scripts.map((row) =>
      row.index === event.scriptIndex
        ? {
            ...row,
            path: event.scriptPath,
            status: event.status,
            durationMs: event.durationMs,
            failedAtStep: event.failedAtStep,
          }
        : row,
    );
    return {
      ...live,
      events,
      scenario: { ...live.scenario, scripts },
    };
  }
  return { ...live, events };
}
