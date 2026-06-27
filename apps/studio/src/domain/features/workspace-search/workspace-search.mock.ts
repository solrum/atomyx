import type {
  WorkspaceSearch,
  WorkspaceSearchHit,
} from "./workspace-search.port.js";

export class MockWorkspaceSearch implements WorkspaceSearch {
  constructor(
    private readonly files: Readonly<Record<string, string>> = {},
  ) {}

  async search(
    _workspacePath: string,
    query: string,
  ): Promise<readonly WorkspaceSearchHit[]> {
    if (query.trim().length < 2) return [];
    const needle = query.toLowerCase();
    const out: WorkspaceSearchHit[] = [];
    for (const [path, content] of Object.entries(this.files)) {
      const lines = content.split("\n");
      for (let i = 0; i < lines.length; i++) {
        const line = lines[i]!;
        if (line.toLowerCase().includes(needle)) {
          out.push({ path, line: i + 1, snippet: line });
        }
      }
    }
    return out;
  }
}
