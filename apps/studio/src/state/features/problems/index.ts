import { useSyncExternalStore } from "react";
import { getFeature } from "../../core/registry.js";
import type {
  Problem,
  ProblemSeverity,
  ProblemsApi,
  ProblemsSnapshot,
} from "./problems.contract.js";
import { problemCounts } from "./problems.contract.js";
import { createZustandProblems } from "./problems.zustand.js";

export type { Problem, ProblemSeverity, ProblemsApi, ProblemsSnapshot };
export { problemCounts };

export const PROBLEMS_KEY = "problems";

export function createProblems(): ProblemsApi {
  return createZustandProblems();
}

export function useProblems(): ProblemsSnapshot &
  Pick<ProblemsApi, "set" | "clear"> {
  const api = getFeature<ProblemsApi>(PROBLEMS_KEY);
  const snap = useSyncExternalStore(api.subscribe, api.getSnapshot, api.getSnapshot);
  return { ...snap, set: api.set, clear: api.clear };
}
