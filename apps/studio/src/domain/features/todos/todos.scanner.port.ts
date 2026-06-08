import type { TodoHit } from "./todos.types.js";

export interface TodoScanner {
  scan(workspacePath: string): Promise<readonly TodoHit[]>;
}
