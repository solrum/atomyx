/**
 * @atomyx/core-driver-mcp — MCP server library for the Atomyx
 * framework. Exports a `createMcpServer` factory plus the tool
 * definitions and pluggable contracts.
 *
 * Usage from a binary entry point (e.g. apps/cli):
 *
 *     import { Orchestra, SystemClock, ConsoleLogger } from "@atomyx/core-driver";
 *     import { IosDriver } from "@atomyx/core-driver-ios";
 *     import { createMcpServer } from "@atomyx/core-driver-mcp";
 *     import { StdioServerTransport } from
 *       "@modelcontextprotocol/sdk/server/stdio.js";
 *
 *     const driver = new IosDriver({ kind: "simulator", udid });
 *     await driver.connect();
 *     const orchestra = new Orchestra({
 *       driver,
 *       clock: new SystemClock(),
 *       logger: new ConsoleLogger("info"),
 *     });
 *     const server = createMcpServer({ orchestra });
 *     await server.connect(new StdioServerTransport());
 *
 * Library callers (Synapse, Studio) use `createMcpServer` the
 * same way but plug in their own transport (HTTP, WebSocket,
 * direct in-process dispatch) instead of stdio.
 *
 * The tool surface is overridable via the `tools` option —
 * import individual tools or replace `DEFAULT_TOOLS` entirely
 * to ship a custom subset.
 */

export { createMcpServer, type McpServerOptions } from "./server.js";
export {
  defineTool,
  type ToolDefinition,
  type ToolContext,
  type AnyToolDefinition,
} from "./tool-definition.js";
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
  inputTextTool,
  swipeTool,
  pressKeyTool,
  screenshotTool,
  waitForElementTool,
} from "./tools/index.js";
