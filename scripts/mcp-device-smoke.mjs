// End-to-end smoke test of the atomyx-driver CLI against a REAL
// Android device. Spawns `node packages/core-driver-cli/dist/main.js
// mcp --platform android --device <serial>` as a child, drives it
// with JSON-RPC over stdio, verifies each response.
//
// Usage:
//   ATOMYX_ANDROID_SERIAL=<serial> node scripts/mcp-device-smoke.mjs
//
// Preconditions on the host:
//   - adb on PATH and the device authorized
//   - Atomyx APK installed + accessibility service enabled
//   - AtomyxForegroundService running (port 8765)
//
// Unlike mcp-stdio-smoke.mjs which uses MockDriver, this test hits
// the real APK over adb forward. Used for post-batch validation
// when a device is available — runs every tool through the real
// pipeline and reports what the device actually returned.
import { spawn } from "node:child_process";
import { once } from "node:events";
import path from "node:path";
import { fileURLToPath } from "node:url";

const serial = process.env.ATOMYX_ANDROID_SERIAL;
if (!serial) {
  console.error("ATOMYX_ANDROID_SERIAL required (e.g. export ATOMYX_ANDROID_SERIAL=RF8...)");
  process.exit(1);
}

const here = path.dirname(fileURLToPath(import.meta.url));
const repo = path.dirname(here);
const binPath = path.join(repo, "packages/core-driver-cli/dist/main.js");

const child = spawn(
  process.execPath,
  [binPath, "mcp", "--platform", "android", "--device", serial, "--log-level", "error"],
  { stdio: ["pipe", "pipe", "pipe"], cwd: repo },
);

const stderrChunks = [];
child.stderr.on("data", (c) => stderrChunks.push(c.toString()));

let buf = "";
const responses = [];
child.stdout.on("data", (chunk) => {
  buf += chunk.toString();
  let nl;
  while ((nl = buf.indexOf("\n")) >= 0) {
    const line = buf.slice(0, nl);
    buf = buf.slice(nl + 1);
    if (line.trim()) {
      try {
        responses.push(JSON.parse(line));
      } catch {}
    }
  }
});

function send(req) {
  child.stdin.write(JSON.stringify(req) + "\n");
}

function waitFor(id, timeoutMs = 15000) {
  return new Promise((resolve, reject) => {
    const deadline = Date.now() + timeoutMs;
    const check = () => {
      const found = responses.find((r) => r.id === id);
      if (found) return resolve(found);
      if (Date.now() > deadline)
        return reject(
          new Error(
            `timeout waiting for id ${id}. stderr: ${stderrChunks.join("").slice(-1000)}`,
          ),
        );
      setTimeout(check, 50);
    };
    check();
  });
}

function must(cond, msg) {
  if (!cond) {
    console.error("FAIL:", msg);
    child.kill("SIGKILL");
    process.exit(1);
  }
}

// Give atomyx-driver time to adb forward + ping /health.
await new Promise((r) => setTimeout(r, 1500));

// 1. initialize
send({
  jsonrpc: "2.0",
  id: 1,
  method: "initialize",
  params: {
    protocolVersion: "2024-11-05",
    capabilities: {},
    clientInfo: { name: "device-smoke", version: "0" },
  },
});
const initResp = await waitFor(1);
must(initResp.result?.serverInfo?.name === "atomyx", "initialize");
console.log(`OK initialize — server=${initResp.result.serverInfo.name}@${initResp.result.serverInfo.version}`);

// 2. list_devices — proves xcrun/adb enumeration works through the tool
send({
  jsonrpc: "2.0",
  id: 2,
  method: "tools/call",
  params: { name: "list_devices", arguments: { platform: "android" } },
});
const devicesResp = await waitFor(2);
const devicesResult = JSON.parse(devicesResp.result?.content?.[0]?.text ?? "{}");
console.log(`OK list_devices — count=${devicesResult.count}`);
if (devicesResult.devices) {
  for (const d of devicesResult.devices.slice(0, 3)) {
    console.log(`  ${d.platform} ${d.kind} ${d.udid} ${d.model ?? ""}`);
  }
}

// 3. get_ui_tree — the big one. Real Android a11y tree.
send({
  jsonrpc: "2.0",
  id: 3,
  method: "tools/call",
  params: { name: "get_ui_tree", arguments: { limit: 50 } },
});
const treeResp = await waitFor(3);
const treeResult = JSON.parse(treeResp.result?.content?.[0]?.text ?? "{}");
must(typeof treeResult.total === "number", "get_ui_tree total field");
console.log(`OK get_ui_tree — total=${treeResult.total} returned=${treeResult.returned} truncated=${treeResult.truncated}`);
// Print a few interesting nodes (with id or text)
const interesting = (treeResult.nodes ?? []).filter(
  (n) => n.id || n.text || n.label,
).slice(0, 5);
for (const n of interesting) {
  console.log(`  [${n.depth}] ${n.role} id=${n.id ?? ""} text=${(n.text ?? "").slice(0, 30)}`);
}

// 4. screenshot — proves bytes flow through the wire
send({
  jsonrpc: "2.0",
  id: 4,
  method: "tools/call",
  params: { name: "screenshot", arguments: {} },
});
const shotResp = await waitFor(4);
const shotResult = JSON.parse(shotResp.result?.content?.[0]?.text ?? "{}");
must(typeof shotResult.base64 === "string" && shotResult.base64.length > 1000, "screenshot base64");
console.log(`OK screenshot — format=${shotResult.format} size=${shotResult.sizeBytes ?? shotResult.base64.length}bytes`);

// 5. list_apps — Android's pm list packages
send({
  jsonrpc: "2.0",
  id: 5,
  method: "tools/call",
  params: { name: "list_apps", arguments: {} },
});
const appsResp = await waitFor(5);
const appsResult = JSON.parse(appsResp.result?.content?.[0]?.text ?? "{}");
must(typeof appsResult.count === "number", "list_apps count");
console.log(`OK list_apps — count=${appsResult.count}`);

// Done — clean shutdown
child.stdin.end();
child.kill("SIGTERM");
await once(child, "exit");
console.log("");
console.log("✓ MCP device smoke passed");
