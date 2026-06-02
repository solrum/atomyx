import type { TodoScanner } from "./scanner.port.js";
import type { TodoHit } from "./types.js";

export class MockTodoScanner implements TodoScanner {
  private readonly map = new Map<string, readonly TodoHit[]>();

  constructor(seeds: Readonly<Record<string, readonly TodoHit[]>> = {}) {
    for (const [k, v] of Object.entries(seeds)) this.map.set(k, v);
  }

  async scan(workspacePath: string): Promise<readonly TodoHit[]> {
    return this.map.get(workspacePath) ?? [];
  }
}
