import { ScreenMirrorDispatcher } from "./mirror.dispatcher.js";
import { ScrcpyScreenMirror } from "./mirror.scrcpy.js";
import { SimctlScreenMirror } from "./mirror.simctl.js";

import type { ScreenMirror } from "../../../domain/features/mirror/mirror.port.js";

/**
 * Assembles the default `ScreenMirror` — one dispatcher that routes
 * by target kind to the scrcpy / simctl adapters. Called once from
 * the composition root. Swapping any adapter (or the dispatcher
 * itself) is a matter of rewriting this factory; consumers hold
 * only the port.
 */
export function createDefaultScreenMirror(): ScreenMirror {
  return new ScreenMirrorDispatcher({
    android: new ScrcpyScreenMirror(),
    "ios-simulator": new SimctlScreenMirror(),
  });
}
