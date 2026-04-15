#!/usr/bin/env node
// Phase 6 — iOS tool layer integration smoke.
//
// Spawns `node dist/index.js` (Atomyx MCP server), drives it via
// JSON-RPC over stdio, and exercises the full stack:
//
//   MCP protocol → ToolFactory → Tool class → strategy (SelectorResolutionPipeline,
//   FuzzyResourceMatcher, AmbiguityDetector, TransitionClassifier)
//   → IosXctestController → TCP → Swift driver → XCUIApplication
//
// Goal: catch integration gaps between tool-layer strategies
// (written during Android development) and the iOS adapter shape.
// If any tool call errors out or returns unexpected data, the
// cross-platform claim of the port abstraction is broken.
//
// Prerequisite: driver running (`make serve`) AND an iPhone sim booted.
//
// Usage: node scripts/ios-mcp-smoke.mjs

import { spawn } from "node:child_process";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

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
        if (msg.error) rj(new Error(`rpc error ${msg.error.code}: ${msg.error.message}`));
        else r(msg.result);
      }
    } catch (e) {
      console.error("[smoke] parse error:", line);
    }
  }
});

function rpc(method, params) {
  const id = nextId++;
  return new Promise((res, rej) => {
    pending.set(id, { resolve: res, reject: rej });
    proc.stdin.write(JSON.stringify({ jsonrpc: "2.0", id, method, params }) + "\n");
    setTimeout(() => {
      if (pending.has(id)) {
        pending.delete(id);
        rej(new Error(`rpc timeout: ${method}`));
      }
    }, 30_000);
  });
}

async function callTool(name, args = {}) {
  const r = await rpc("tools/call", { name, arguments: args });
  if (r.isError) {
    throw new Error(`tool ${name} error: ${r.content?.[0]?.text ?? "unknown"}`);
  }
  return JSON.parse(r.content[0].text);
}

function header(msg) {
  console.log(`\n\x1b[34m== ${msg} ==\x1b[0m`);
}
function log(msg) { console.log(`[smoke] ${msg}`); }
function green(msg) { console.log(`\x1b[32m✓\x1b[0m ${msg}`); }
function fail(msg) { throw new Error(msg); }

async function main() {
  header("initialize MCP");
  await rpc("initialize", {
    protocolVersion: "2024-11-05",
    capabilities: {},
    clientInfo: { name: "ios-mcp-smoke", version: "0.0.1" },
  });
  proc.stdin.write(JSON.stringify({ jsonrpc: "2.0", method: "notifications/initialized" }) + "\n");
  green("MCP initialized");

  header("tools/list");
  const toolList = await rpc("tools/list", {});
  green(`${toolList.tools.length} tools registered`);
  const expected = [
    "list_devices", "select_device", "launch_app", "list_apps",
    "get_ui_tree", "find_element", "get_screenshot",
    "tap", "tap_and_wait_transition", "input_text", "swipe", "press_key",
    "wait_for_element", "start_run", "finish_run", "report_bug",
    "get_playbook", "add_case_study", "get_case_studies",
  ];
  const registered = new Set(toolList.tools.map((t) => t.name));
  const missing = expected.filter((n) => !registered.has(n));
  if (missing.length) fail(`missing tools: ${missing.join(", ")}`);
  green("all 19 tools present");

  header("list_devices");
  const devs = await callTool("list_devices");
  const list = devs.devices ?? devs;
  log(`devices: ${JSON.stringify(list)}`);
  const iosDev = Array.isArray(list) ? list.find((d) => d.platform === "ios") : null;
  if (!iosDev) fail("no iOS device in list — is a simulator booted?");
  green(`iOS device: ${iosDev.id} (${iosDev.model ?? iosDev.serial})`);

  header(`select_device ${iosDev.id}`);
  const sel = await callTool("select_device", { deviceId: iosDev.id });
  log(`selected: ${JSON.stringify(sel)}`);
  green("selected");

  header("launch_app com.apple.Preferences");
  const launched = await callTool("launch_app", { appId: "com.apple.Preferences" });
  log(`launch result: ${JSON.stringify(launched).slice(0, 200)}`);
  green("launched");

  header("get_ui_tree");
  const tree = await callTool("get_ui_tree", { stableOnly: true, limit: 20 });
  log(`tree: count=${tree.count ?? tree.elementCount} total=${tree.totalAvailable ?? tree.total} truncated=${tree.truncated}`);
  if (!tree.tree || typeof tree.tree !== "string" || tree.tree.length === 0) {
    fail(`get_ui_tree returned empty tree string: ${JSON.stringify(tree)}`);
  }
  green(`tree rendered (${tree.tree.split("\n").length} lines)`);
  // Print first 5 lines of rendered tree for visual check
  for (const line of tree.tree.split("\n").slice(0, 5)) {
    console.log(`    ${line}`);
  }

  // NOTE on selector choice: `contentDesc: "General"` is ambiguous on
  // iOS Settings root because the label appears both on the button
  // (the tappable row with resourceId="com.apple.settings.general")
  // AND on an inner staticText child with just contentDesc="General".
  // AmbiguityDetector correctly flags this as `found:false` with a
  // `candidates[]` list — the smoke uses the stable `resourceId`
  // directly instead, which uniquely picks the button. This mirrors
  // the production guidance: always prefer resourceId when available.
  header("find_element {resourceId: 'com.apple.settings.general'}");
  const found = await callTool("find_element", { resourceId: "com.apple.settings.general" });
  log(`find result: ${JSON.stringify(found).slice(0, 300)}`);
  if (!found.found) fail(`find_element did not locate General: ${JSON.stringify(found)}`);
  green(`found role=${found.role} label="${found.label}" center=${JSON.stringify(found.center)}`);

  // `tap` tool requires the selector wrapped in the `selector` key
  // (vs `find_element` which accepts flat selector fields as the
  // top-level arg shape). Different schemas because `tap` ALSO
  // accepts `{x, y}` raw coords — the wrapping disambiguates intent.
  header("tap {selector: {resourceId: 'com.apple.settings.general'}}");
  const tapped = await callTool("tap", {
    selector: { resourceId: "com.apple.settings.general" },
  });
  log(`tap result: ${JSON.stringify(tapped).slice(0, 200)}`);
  if (!tapped.ok) fail(`tap failed: ${tapped.reason}`);
  green(`tap ok — ${tapped.reason ?? ""}`);

  // Wait for navigation animation
  await new Promise((r) => setTimeout(r, 800));

  header("get_ui_tree after nav");
  const tree2 = await callTool("get_ui_tree", { stableOnly: true, limit: 20 });
  log(`post-nav: count=${tree2.count ?? tree2.elementCount}`);
  green(`tree dump #2 (${tree2.tree?.split("\n").length ?? 0} lines)`);

  header('press_key "back"');
  const backed = await callTool("press_key", { key: "back" });
  log(`press_key result: ${JSON.stringify(backed)}`);
  if (backed.ok !== undefined) {
    green(`press_key returns ActionResult ok=${backed.ok} reason="${backed.reason ?? ""}"`);
  } else {
    fail(`press_key should return ActionResult {ok, reason} — got: ${JSON.stringify(backed)}`);
  }

  header("get_screenshot");
  const shot = await callTool("get_screenshot");
  const byteLen = shot.bytes ?? shot.base64?.length ?? 0;
  log(`screenshot bytes=${byteLen}`);
  if (byteLen === 0) fail("screenshot returned 0 bytes");
  green("screenshot captured");

  console.log("\n\x1b[32m═══════════════════════════════════════\x1b[0m");
  console.log("\x1b[32m  iOS MCP smoke PASSED — tool layer ↔ iOS adapter ↔ driver ↔ sim\x1b[0m");
  console.log("\x1b[32m═══════════════════════════════════════\x1b[0m");

  proc.kill();
  process.exit(0);
}

main().catch((err) => {
  console.error(`\n\x1b[31m✗ ${err.message}\x1b[0m`);
  if (err.stack) console.error(err.stack);
  proc.kill();
  process.exit(1);
});
