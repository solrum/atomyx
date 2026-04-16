#!/usr/bin/env node
/**
 * `atomyx-mcp` — MCP stdio server for Atomyx.
 *
 * Structurally identical to a minimal handwritten MCP server:
 * synchronous Server construction + immediate stdio connect.
 * No async work before the transport is live.
 */
import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import {
  CallToolRequestSchema,
  ListToolsRequestSchema,
} from "@modelcontextprotocol/sdk/types.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { SystemClock, ConsoleLogger, NoopLogger } from "@atomyx/core-driver";
import { IosDriver } from "@atomyx/core-driver-ios";
import { AndroidDriver } from "@atomyx/core-driver-android";
import { DeviceSession } from "./device-session.js";
import { DEFAULT_TOOLS } from "./tools/index.js";
import { zodToJsonSchema } from "./zod-to-json-schema.js";
import type { ToolContext } from "./tool-definition.js";
import { FileStorage, RunStore, InMemoryStorage } from "@atomyx/core-driver";

// ── Arg parsing (minimal) ──────────────────────────────────────

let logLevel: "debug" | "info" | "warn" | "error" = "info";
let listTools = false;
let listToolsJson = false;

for (let i = 2; i < process.argv.length; i++) {
  const a = process.argv[i]!;
  if (a === "--help" || a === "-h") {
    process.stderr.write(
      "atomyx-mcp — Atomyx MCP stdio server\n\n" +
        "  atomyx-mcp [--log-level info] [--list-tools] [--list-tools-json]\n\n" +
        "No platform flags. Agent picks device via select_device tool.\n",
    );
    process.exit(0);
  }
  if (a === "--list-tools") listTools = true;
  if (a === "--list-tools-json") listToolsJson = true;
  if (a === "--log-level" && process.argv[i + 1]) {
    logLevel = process.argv[++i] as typeof logLevel;
  }
}

// ── Inspect mode ───────────────────────────────────────────────

if (listToolsJson) {
  process.stdout.write(
    JSON.stringify(
      { tools: DEFAULT_TOOLS.map((t) => ({ name: t.name, description: t.description })) },
      null,
      2,
    ) + "\n",
  );
  process.exit(0);
}

if (listTools) {
  process.stdout.write(`atomyx-mcp — ${DEFAULT_TOOLS.length} tools\n\n`);
  for (const t of DEFAULT_TOOLS) {
    process.stdout.write(`  ${t.name}\n      ${t.description.slice(0, 120)}\n\n`);
  }
  process.exit(0);
}

// ── Server construction (SYNCHRONOUS — no await) ───────────────

const logger = (logLevel as string) === "error" ? new NoopLogger() : new ConsoleLogger(logLevel);
const clock = new SystemClock();

const session = new DeviceSession({
  factories: {
    ios: (id, opts) =>
      new IosDriver({
        kind: opts.kind ?? "simulator",
        udid: id,
        port: opts.port,
        autoLaunch: true,
      }),
    android: (id) => new AndroidDriver({ serial: id }),
  },
  clock,
  logger,
});

const storage =
  process.env.ATOMYX_STORAGE === "memory"
    ? new InMemoryStorage()
    : new FileStorage();
const runStore = new RunStore();

const ctx: ToolContext = { session, logger, storage, runStore, clock };

// Pre-compute tool descriptors for tools/list response.
const toolDescriptors = DEFAULT_TOOLS.map((t) => ({
  name: t.name,
  description: t.description,
  inputSchema: zodToJsonSchema(t.inputSchema),
}));

const byName = new Map(DEFAULT_TOOLS.map((t) => [t.name, t]));

// Server + handlers — direct construction, same shape as minimal-mcp.
const server = new Server(
  { name: "atomyx", version: "0.1.0" },
  { capabilities: { tools: {} } },
);

server.setRequestHandler(ListToolsRequestSchema, async () => ({
  tools: toolDescriptors,
}));

server.setRequestHandler(CallToolRequestSchema, async (req) => {
  const tool = byName.get(req.params.name);
  if (!tool) {
    return {
      isError: true,
      content: [{ type: "text", text: `Unknown tool: ${req.params.name}` }],
    };
  }
  const parsed = tool.inputSchema.safeParse(req.params.arguments ?? {});
  if (!parsed.success) {
    return {
      isError: true,
      content: [
        { type: "text", text: `Invalid arguments for ${tool.name}: ${parsed.error.message}` },
      ],
    };
  }
  try {
    const result = await tool.execute(parsed.data, ctx);
    return {
      content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
    };
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    return {
      isError: true,
      content: [{ type: "text", text: msg }],
    };
  }
});

// ── Shutdown ────────────────────────────────────────────────────

process.on("SIGINT", () => {
  session.disconnect().catch(() => {});
  process.exit(130);
});
process.on("SIGTERM", () => {
  session.disconnect().catch(() => {});
  process.exit(0);
});

// ── Transport connect (FIRST await) ────────────────────────────

const transport = new StdioServerTransport();
await server.connect(transport);
