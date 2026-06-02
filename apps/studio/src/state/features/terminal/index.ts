import type { TerminalPort } from "../../../domain/features/terminal/terminal.port.js";
import type {
  TerminalApi,
  TerminalSession,
  TerminalSpawnOptions,
} from "./terminal.contract.js";
import { createTerminalImpl } from "./terminal.impl.js";

export type { TerminalApi, TerminalSession, TerminalSpawnOptions };

export const TERMINAL_KEY = "terminal";

export function createTerminal(deps: { readonly port: TerminalPort }): TerminalApi {
  return createTerminalImpl(deps);
}

