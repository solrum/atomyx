import type {
  TerminalSession,
  TerminalSpawnOptions,
} from "../../../domain/features/terminal/terminal.port.js";

export type { TerminalSession, TerminalSpawnOptions };

export interface TerminalApi {
  spawn(
    opts: TerminalSpawnOptions,
    onData: (data: string) => void,
  ): Promise<TerminalSession>;
}
