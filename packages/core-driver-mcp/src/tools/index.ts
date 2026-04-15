/**
 * Tool registry — a single ordered list of every tool the
 * default MCP server exposes. Adding a new tool = create the
 * file, append it here. The order is the order tools appear in
 * the `tools/list` MCP response, which influences how the agent
 * scans them; put high-frequency tools first.
 *
 * This list is intentionally NOT auto-discovered from the file
 * system. Explicit registration:
 *   - keeps the server entry point dependency-free at runtime
 *   - lets feature consumers cherry-pick a subset (e.g. Studio
 *     might want a different tool surface than the MCP server)
 *   - prevents accidental new files from leaking onto the agent
 */

import type { AnyToolDefinition } from "../tool-definition.js";
import { launchAppTool } from "./launch-app.tool.js";
import { getUiTreeTool } from "./get-ui-tree.tool.js";
import { findElementTool } from "./find-element.tool.js";
import { tapTool } from "./tap.tool.js";
import { inputTextTool } from "./input-text.tool.js";
import { swipeTool } from "./swipe.tool.js";
import { pressKeyTool } from "./press-key.tool.js";
import { screenshotTool } from "./screenshot.tool.js";
import { waitForElementTool } from "./wait-for-element.tool.js";

export const DEFAULT_TOOLS: readonly AnyToolDefinition[] = [
  launchAppTool as unknown as AnyToolDefinition,
  getUiTreeTool as unknown as AnyToolDefinition,
  findElementTool as unknown as AnyToolDefinition,
  tapTool as unknown as AnyToolDefinition,
  inputTextTool as unknown as AnyToolDefinition,
  swipeTool as unknown as AnyToolDefinition,
  pressKeyTool as unknown as AnyToolDefinition,
  screenshotTool as unknown as AnyToolDefinition,
  waitForElementTool as unknown as AnyToolDefinition,
];

export {
  launchAppTool,
  getUiTreeTool,
  findElementTool,
  tapTool,
  inputTextTool,
  swipeTool,
  pressKeyTool,
  screenshotTool,
  waitForElementTool,
};
