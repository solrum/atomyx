/**
 * @atomyx/mcp — MCP server library for the Atomyx
 * framework. Exports a `createMcpServer` factory, the runtime
 * `DeviceSession` container, host-side device discovery, tool
 * definitions, and pluggable contracts.
 *
 * ## Usage
 *
 * The server is driven by a `DeviceSession` that holds the
 * currently-active device (if any) and swaps drivers on
 * `select_device` tool calls. Binary consumers (`atomyx-mcp`)
 * build the session with a driver-factory map and let the agent
 * pick devices at runtime:
 *
 *     import { createMcpServer, DeviceSession } from "@atomyx/mcp";
 *     import { IosDriver } from "@atomyx/ios-driver";
 *     import { AndroidDriver } from "@atomyx/android-driver";
 *     import { StdioServerTransport } from
 *       "@modelcontextprotocol/sdk/server/stdio.js";
 *
 *     const session = new DeviceSession({
 *       factories: {
 *         ios: (id, opts) => new IosDriver({
 *           kind: opts.kind ?? "simulator",
 *           udid: id,
 *           port: opts.port,
 *         }),
 *         android: (id) => new AndroidDriver({ serial: id }),
 *       },
 *     });
 *     const server = createMcpServer({ session });
 *     await server.connect(new StdioServerTransport());
 *
 * Library consumers embed the server with their own driver
 * factories — e.g. a `MockDriver` for replay, or a remote-
 * control driver for cloud test farms.
 *
 * The tool surface is overridable via the `tools` option —
 * import individual tools or replace `DEFAULT_TOOLS` entirely
 * to ship a custom subset.
 */

export { createMcpServer, type McpServerOptions } from "./server.js";
export {
  defineTool,
  orchestraOrFail,
  type ToolDefinition,
  type ToolContext,
  type AnyToolDefinition,
} from "./tool-definition.js";
export {
  DeviceSession,
  type DeviceSessionDeps,
  type DriverFactory,
  type DriverSelectOptions,
  type SelectDeviceInput,
  type ActiveDevice,
} from "./device-session.js";
export {
  discoverDevices,
  autoSelectDevice,
  AutoSelectError,
  type DiscoveredDevice,
  type DiscoverOptions,
} from "./device-discovery.js";
export {
  SelectorSchema,
  compileSelectorInput,
  type SelectorInput,
} from "./selector-schema.js";
export { zodToJsonSchema } from "./zod-to-json-schema.js";
export {
  DEFAULT_TOOLS,
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
} from "./tools/index.js";
export {
  DEFAULT_PROMPTS,
  playbookPrompt,
  exploratoryPrompt,
  regressionPrompt,
  bugReproPrompt,
  definePrompt,
  interpolate,
  type PromptDefinition,
  type PromptArgument,
  type PromptMessage,
} from "./prompts/index.js";
