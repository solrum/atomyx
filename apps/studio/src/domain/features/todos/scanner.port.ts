import type { TodoHit } from "./types.js";

export interface TodoScanner {
  scan(workspacePath: string): Promise<readonly TodoHit[]>;
}
