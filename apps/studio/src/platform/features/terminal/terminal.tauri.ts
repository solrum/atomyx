import { Channel, invoke } from "@tauri-apps/api/core";
import type {
  TerminalPort,
  TerminalSession,
  TerminalSpawnOptions,
} from "../../../domain/features/terminal/terminal.port.js";

/**
 * Tauri-backed TerminalPort. Spawns a PTY through the
 * `terminal_spawn` Rust command and streams bytes back via a
 * Tauri `Channel`. Write/resize/kill route through the matching
 * `terminal_write`, `terminal_resize`, `terminal_kill` commands.
 */
export class TauriTerminalPort implements TerminalPort {
  async spawn(
    opts: TerminalSpawnOptions,
    onData: (data: string) => void,
  ): Promise<TerminalSession> {
    const channel = new Channel<string>();
    channel.onmessage = (data) => onData(data);
    const id = await invoke<string>("terminal_spawn", {
      cols: opts.cols,
      rows: opts.rows,
      workspacePath: opts.workspacePath,
      onData: channel,
    });
    return {
      id,
      write: (data) => {
        void invoke("terminal_write", { id, data });
      },
      resize: (cols, rows) => {
        void invoke("terminal_resize", { id, cols, rows });
      },
      kill: () => {
        void invoke("terminal_kill", { id });
      },
    };
  }
}
