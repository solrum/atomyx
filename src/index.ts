#!/usr/bin/env node
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { createServer } from "./server.js";

async function main() {
  const server = createServer();
  const transport = new StdioServerTransport();
  await server.connect(transport);
  console.error("[adet] MCP server connected via stdio");
}

main().catch((err) => {
  console.error("[adet] fatal:", err);
  process.exit(1);
});
