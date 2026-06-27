import { invoke } from "@tauri-apps/api/core";
import type { TodoScanner } from "../../../domain/features/todos/index.js";
import type { TodoHit } from "../../../domain/features/todos/index.js";

export class TauriTodoScanner implements TodoScanner {
  async scan(workspacePath: string): Promise<readonly TodoHit[]> {
    return invoke<readonly TodoHit[]>("workspace_scan_todos", {
      workspacePath,
    });
  }
}
