#!/usr/bin/env node
// Smoke test for adet MCP server via stdio protocol.
// Spawns the server, sends a sequence of JSON-RPC requests, asserts results.
//
// Run: node apps/adet/scripts/smoke-mcp.mjs

import { spawn } from "node:child_process";
import { fileURLToPath } from "node:url";
import { dirname, resolve } from "node:path";

const __dirname = dirname(fileURLToPath(import.meta.url));
const serverEntry = resolve(__dirname, "../dist/index.js");

const proc = spawn("node", [serverEntry], {
  stdio: ["pipe", "pipe", "inherit"],
});

let buffer = "";
const pending = new Map();
let nextId = 1;

proc.stdout.on("data", (chunk) => {
  buffer += chunk.toString();
  let nl;
  while ((nl = buffer.indexOf("\n")) >= 0) {
    const line = buffer.slice(0, nl).trim();
    buffer = buffer.slice(nl + 1);
    if (!line) continue;
    try {
      const msg = JSON.parse(line);
      if (msg.id != null && pending.has(msg.id)) {
        const { resolve: r, reject: rj } = pending.get(msg.id);
        pending.delete(msg.id);
        if (msg.error) rj(new Error(`${msg.error.code}: ${msg.error.message}`));
        else r(msg.result);
      }
    } catch (e) {
      console.error("parse error:", line);
    }
  }
});

function send(method, params) {
  const id = nextId++;
  return new Promise((res, rej) => {
    pending.set(id, { resolve: res, reject: rej });
    proc.stdin.write(JSON.stringify({ jsonrpc: "2.0", id, method, params }) + "\n");
    setTimeout(() => {
      if (pending.has(id)) {
        pending.delete(id);
        rej(new Error(`timeout: ${method}`));
      }
    }, 15000);
  });
}

function callTool(name, args = {}) {
  return send("tools/call", { name, arguments: args }).then((r) => {
    if (r.isError) throw new Error(`tool error: ${r.content?.[0]?.text}`);
    return JSON.parse(r.content[0].text);
  });
}

const green = (s) => `\x1b[32m${s}\x1b[0m`;
const red = (s) => `\x1b[31m${s}\x1b[0m`;
const blue = (s) => `\x1b[34m${s}\x1b[0m`;

async function main() {
  console.log(blue("→ initialize"));
  await send("initialize", {
    protocolVersion: "2024-11-05",
    capabilities: {},
    clientInfo: { name: "smoke-mcp", version: "0.0.1" },
  });
  proc.stdin.write(JSON.stringify({ jsonrpc: "2.0", method: "notifications/initialized" }) + "\n");
  console.log(green("✓ initialized"));

  console.log(blue("→ tools/list"));
  const tools = await send("tools/list", {});
  console.log(green(`✓ ${tools.tools.length} tools registered`));

  console.log(blue("→ list_devices"));
  const devs = await callTool("list_devices");
  if (!devs.devices?.length) throw new Error("no devices");
  console.log(green(`✓ ${devs.devices.length} device(s): ${devs.devices.map((d) => d.id).join(", ")}`));
  const target = devs.devices[0];

  console.log(blue(`→ select_device ${target.id}`));
  const sel = await callTool("select_device", { deviceId: target.id });
  console.log(green(`✓ selected ${sel.selected} (${sel.platform})`));

  console.log(blue("→ get_ui_tree"));
  const tree = await callTool("get_ui_tree");
  console.log(green(`✓ ${tree.elementCount} elements`));
  console.log("  preview:", tree.tree.split("\n").slice(0, 5).join("\n  "));

  console.log(blue("→ current_activity"));
  const act = await callTool("current_activity");
  console.log(green(`✓ ${act.packageName} / ${act.activity} (${act.source})`));

  console.log(blue("→ get_screenshot"));
  const shot = await callTool("get_screenshot");
  console.log(green(`✓ screenshot saved ${shot.bytes} bytes → ${shot.path}`));

  console.log(blue("→ get_ui_tree (compact)"));
  const compact = await callTool("get_ui_tree", { format: "compact" });
  console.log(green(`✓ compact ${compact.count} elements`));

  console.log(blue("→ resolve_selector {contentDesc: ログイン}"));
  const resolved = await callTool("resolve_selector", { selector: { contentDesc: "ログイン" } });
  console.log(green(`✓ resolved=${resolved.found} via ${resolved.resolvedBy ?? "n/a"}`));

  console.log(blue("→ tap selector {contentDesc: ログイン}"));
  const tapResult = await callTool("tap", { selector: { contentDesc: "ログイン" } });
  console.log(green(`✓ tap ok=${tapResult.ok} reason=${tapResult.reason}`));

  console.log("\n" + green("═══════════════════════════════════════"));
  console.log(green("  All MCP smoke tests passed ✓"));
  console.log(green("═══════════════════════════════════════"));

  proc.kill();
  process.exit(0);
}

main().catch((err) => {
  console.error(red(`✗ ${err.message}`));
  proc.kill();
  process.exit(1);
});
