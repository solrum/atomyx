/**
 * Tool registry — a single ordered list of every tool the
 * default MCP server exposes. Adding a new tool = create the
 * file, append it here.
 *
 * Order matches the order tools appear in the `tools/list`
 * MCP response. High-frequency tools come first so the agent
 * scans them before less-common ones.
 *
 * Registration is explicit (not auto-discovered from the file
 * system) so:
 *   - accidental new files don't leak onto the agent surface
 *   - feature consumers can cherry-pick a subset via the
 *     `createMcpServer({tools: [...]})` option
 *   - the dependency graph is visible at this file level
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
import { tapAndWaitTransitionTool } from "./tap-and-wait-transition.tool.js";
import { startRunTool, finishRunTool } from "./run-lifecycle.tool.js";
import {
  listRunsTool,
  getRunTool,
  deleteRunTool,
  updateRunSummaryTool,
} from "./run-read.tool.js";
import { reportBugTool } from "./report-bug.tool.js";
import { listBugsTool, getBugTool, deleteBugTool } from "./bug-read.tool.js";
import { addCaseStudyTool, getCaseStudiesTool } from "./case-study.tool.js";
import { listAppsTool } from "./list-apps.tool.js";
import { listDevicesTool } from "./list-devices.tool.js";
import {
  selectDeviceTool,
  disconnectDeviceTool,
} from "./select-device.tool.js";
import { runScriptTool } from "./run-script.tool.js";

export const DEFAULT_TOOLS: readonly AnyToolDefinition[] = [
  // Device + app lifecycle (called first in most sessions)
  listDevicesTool as unknown as AnyToolDefinition,
  selectDeviceTool as unknown as AnyToolDefinition,
  disconnectDeviceTool as unknown as AnyToolDefinition,
  listAppsTool as unknown as AnyToolDefinition,
  launchAppTool as unknown as AnyToolDefinition,

  // Orientation + discovery
  getUiTreeTool as unknown as AnyToolDefinition,
  findElementTool as unknown as AnyToolDefinition,
  screenshotTool as unknown as AnyToolDefinition,

  // Core actions
  tapTool as unknown as AnyToolDefinition,
  tapAndWaitTransitionTool as unknown as AnyToolDefinition,
  inputTextTool as unknown as AnyToolDefinition,
  swipeTool as unknown as AnyToolDefinition,
  pressKeyTool as unknown as AnyToolDefinition,

  // Wait + verification
  waitForElementTool as unknown as AnyToolDefinition,

  // Run lifecycle + reporting
  startRunTool as unknown as AnyToolDefinition,
  finishRunTool as unknown as AnyToolDefinition,
  reportBugTool as unknown as AnyToolDefinition,

  // Run + bug queries (read-only)
  listRunsTool as unknown as AnyToolDefinition,
  getRunTool as unknown as AnyToolDefinition,
  listBugsTool as unknown as AnyToolDefinition,
  getBugTool as unknown as AnyToolDefinition,

  // Run + bug CRUD (mutating)
  updateRunSummaryTool as unknown as AnyToolDefinition,
  deleteRunTool as unknown as AnyToolDefinition,
  deleteBugTool as unknown as AnyToolDefinition,

  // Guidance / case studies
  addCaseStudyTool as unknown as AnyToolDefinition,
  getCaseStudiesTool as unknown as AnyToolDefinition,

  // Script execution
  runScriptTool as unknown as AnyToolDefinition,
];

export {
  launchAppTool,
  getUiTreeTool,
  findElementTool,
  tapTool,
  tapAndWaitTransitionTool,
  inputTextTool,
  swipeTool,
  pressKeyTool,
  screenshotTool,
  waitForElementTool,
  startRunTool,
  finishRunTool,
  listRunsTool,
  getRunTool,
  updateRunSummaryTool,
  deleteRunTool,
  reportBugTool,
  listBugsTool,
  getBugTool,
  deleteBugTool,
  addCaseStudyTool,
  getCaseStudiesTool,
  listAppsTool,
  listDevicesTool,
  selectDeviceTool,
  disconnectDeviceTool,
  runScriptTool,
};
