import type { AnyCommandDefinition } from "@atomyx/driver/script";
import { launchAppCommand } from "./launch-app.command.js";
import { tapCommand } from "./tap.command.js";
import { typeCommand } from "./type.command.js";
import { waitForCommand } from "./wait-for.command.js";
import { assertVisibleCommand } from "./assert-visible.command.js";
import { assertNotVisibleCommand } from "./assert-not-visible.command.js";
import { screenshotCommand } from "./screenshot.command.js";
import { swipeCommand } from "./swipe.command.js";
import { pressKeyCommand } from "./press-key.command.js";
import { backCommand } from "./back.command.js";
import { sleepCommand } from "./sleep.command.js";
import { captureCommand } from "./capture.command.js";
import { assertApiCommand } from "./assert-api.command.js";
import { extractCommand } from "./extract.command.js";
import { handleCommand } from "./handle.command.js";
import { branchCommand } from "./branch.command.js";
import { runFlowCommand } from "./run-flow.command.js";
import { pointerCommand } from "./pointer.command.js";

/**
 * Built-in command registry. Manually ordered, explicitly
 * registered — same pattern as DEFAULT_TOOLS in core-driver-mcp.
 *
 * Adding a new command:
 * 1. Create `commands/<name>.command.ts` with `defineCommand`
 * 2. Import and append here
 * 3. Re-export below
 */
export const DEFAULT_COMMANDS: readonly AnyCommandDefinition[] = [
  launchAppCommand as AnyCommandDefinition,
  tapCommand as AnyCommandDefinition,
  typeCommand as AnyCommandDefinition,
  waitForCommand as AnyCommandDefinition,
  assertVisibleCommand as AnyCommandDefinition,
  assertNotVisibleCommand as AnyCommandDefinition,
  screenshotCommand as AnyCommandDefinition,
  swipeCommand as AnyCommandDefinition,
  pressKeyCommand as AnyCommandDefinition,
  backCommand as AnyCommandDefinition,
  sleepCommand as AnyCommandDefinition,
  captureCommand as AnyCommandDefinition,
  assertApiCommand as AnyCommandDefinition,
  extractCommand as AnyCommandDefinition,
  handleCommand as AnyCommandDefinition,
  branchCommand as AnyCommandDefinition,
  runFlowCommand as AnyCommandDefinition,
  pointerCommand as AnyCommandDefinition,
];

export {
  launchAppCommand,
  tapCommand,
  typeCommand,
  waitForCommand,
  assertVisibleCommand,
  assertNotVisibleCommand,
  screenshotCommand,
  swipeCommand,
  pressKeyCommand,
  backCommand,
  sleepCommand,
  captureCommand,
  assertApiCommand,
  extractCommand,
  handleCommand,
  branchCommand,
  runFlowCommand,
  pointerCommand,
};
