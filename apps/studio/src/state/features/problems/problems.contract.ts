export type ProblemSeverity = "error" | "warning" | "info" | "hint";

export interface Problem {
  readonly path: string;
  readonly line: number;
  readonly column: number;
  readonly severity: ProblemSeverity;
  readonly message: string;
  readonly source: string | null;
}

export interface ProblemsSnapshot {
  readonly items: readonly Problem[];
}

export interface ProblemsApi {
  getSnapshot(): ProblemsSnapshot;
  subscribe(listener: () => void): () => void;
  set(items: readonly Problem[]): void;
  clear(): void;
}

export function problemCounts(items: readonly Problem[]): {
  readonly errors: number;
  readonly warnings: number;
  readonly others: number;
} {
  let errors = 0;
  let warnings = 0;
  let others = 0;
  for (const p of items) {
    if (p.severity === "error") errors++;
    else if (p.severity === "warning") warnings++;
    else others++;
  }
  return { errors, warnings, others };
}
