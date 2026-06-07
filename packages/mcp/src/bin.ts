#!/usr/bin/env node
/**
 * `atomyx mcp` — MCP stdio server for Atomyx.
 *
 * Thin entry-point that wires the workspace's concrete driver
 * factories (iOS + Android) into a `DeviceSession` and hands the
 * session to `createMcpServer`. All request dispatch, structured
 * timeout enforcement, and error envelopes live in
 * `createMcpServer` so the library API and this binary share one
 * implementation.
 */
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import {
  SystemClock,
  ConsoleLogger,
  NoopLogger,
  FileStorage,
  RunStore,
  InMemoryStorage,
} from "@atomyx/driver";
import { IosDriver } from "@atomyx/ios-driver";
import { AndroidDriver } from "@atomyx/android-driver";
import { DeviceSession } from "./device-session.js";
import { DEFAULT_TOOLS } from "./tools/index.js";
import { createMcpServer } from "./server.js";

// ── Arg parsing (minimal) ──────────────────────────────────────

let logLevel: "debug" | "info" | "warn" | "error" = "info";
let listTools = false;
let listToolsJson = false;

for (let i = 2; i < process.argv.length; i++) {
  const a = process.argv[i]!;
  if (a === "--help" || a === "-h") {
    process.stderr.write(
      "atomyx mcp — Atomyx MCP stdio server\n\n" +
        "  atomyx mcp [--log-level info] [--list-tools] [--list-tools-json]\n\n" +
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
  process.stdout.write(`atomyx mcp — ${DEFAULT_TOOLS.length} tools\n\n`);
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

const server = createMcpServer({
  session,
  logger,
  storage,
  runStore,
  clock,
  instructions: `You are connected to Atomyx — a mobile test orchestration framework that drives real iOS and Android devices.

When the user asks you to test, verify, check, or interact with a mobile app, use the Atomyx tools:

1. First call list_devices to see available devices.
2. Call select_device to bind to a device.
3. Call launch_app with the app's bundle id (iOS) or package name (Android).
4. Use get_ui_tree to see what's on screen before acting.
5. Use tap, input_text, swipe, press_key to interact with the app.
6. Use wait_for_element to verify navigation worked.
7. Use screenshot to capture visual evidence.

Keywords that should trigger Atomyx tools: test, mobile, app, device, tap, swipe, screenshot, UI, screen, launch, install, Android, iOS, phone, simulator, emulator.

When running a scripted test, use run_script with a YAML test script.

Always orient first (get_ui_tree), then act, then verify.

If a tool returns isError=true with a payload like {"code":"TOOL_TIMEOUT", ...}, the device call exceeded its budget and was aborted. Do NOT retry the same tool immediately — re-orient with get_ui_tree (it will fail fast if the driver is dead), or call select_device again to reconnect. The payload's "hint" field describes the recommended next step.

Before each tool call, short briefly explain what you are about to do and why. After each tool call, short summarize the result. Never skip explanation between steps.

Before starting a mobile testing task, check whether the consumer project has Atomyx workflow skills installed in .claude/skills/ (placed there by atomyx init). If present, load the relevant ones: atomyx-test-loop (structured orient/act/verify loop), atomyx-debug-failure (recovery from tool errors and stale UI trees), and atomyx-script-authoring (capture successful flows as replayable YAML so future sessions resume from the same screen without re-discovering the path). These skills live in the consumer project, not in this server — load them from .claude/skills/ when they exist.`,
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
