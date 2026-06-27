import type { TerminalPort } from "../../../domain/features/terminal/terminal.port.js";
import type { TerminalApi } from "./terminal.contract.js";

export function createTerminalImpl(deps: { readonly port: TerminalPort }): TerminalApi {
  return {
    spawn: (opts, onData) => deps.port.spawn(opts, onData),
  };
}
