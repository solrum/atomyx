export interface WorkspaceSearchHit {
  readonly path: string;
  readonly line: number;
  readonly snippet: string;
}

/**
 * Grep-in-workspace capability. Reads every text file under the
 * opened workspace root and returns line-level matches. A remote
 * or indexed backend can later implement the same contract
 * without touching the command-palette UI.
 */
export interface WorkspaceSearch {
  search(
    workspacePath: string,
    query: string,
  ): Promise<readonly WorkspaceSearchHit[]>;
}
