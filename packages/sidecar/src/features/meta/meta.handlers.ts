import type { Dispatcher } from "../../infra/transport/dispatcher.js";

export const PROTOCOL_VERSION = 1;

/**
 * Version + method-discovery handlers. Hosts use `protocolVersion`
 * to reject sidecars that are too new or too old for them, and
 * `listMethods` for debugging / tooling.
 */
export function registerMetaHandlers(dispatcher: Dispatcher): void {
  dispatcher.register("protocolVersion", () => ({
    version: PROTOCOL_VERSION,
  }));
  dispatcher.register("listMethods", () => ({
    methods: dispatcher.methods(),
  }));
  dispatcher.register("ping", () => ({ ok: true }));
}
