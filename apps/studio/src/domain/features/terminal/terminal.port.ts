export interface TerminalSpawnOptions {
  readonly cols: number;
  readonly rows: number;
  readonly workspacePath: string | null;
}

export interface TerminalSession {
  readonly id: string;
  write(data: string): void;
  resize(cols: number, rows: number): void;
  kill(): void;
}

export interface TerminalPort {
  /**
   * Spawn a PTY-backed shell process and return a handle for writing
   * input, resizing, and killing the session. `onData` is invoked on
   * every byte the shell emits (stdout + stderr, merged by the PTY).
   */
  spawn(
    opts: TerminalSpawnOptions,
    onData: (data: string) => void,
  ): Promise<TerminalSession>;
}
