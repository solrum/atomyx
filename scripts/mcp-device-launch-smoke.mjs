// Second-round device smoke: launches a real app (Settings), waits
// for the screen to settle, dumps the tree, resolves a selector,
// and runs the full run-lifecycle with report_bug + list_bugs +
// delete_bug on storage. Builds on mcp-device-smoke.mjs.
//
// Usage:
//   ATOMYX_ANDROID_SERIAL=<serial> node scripts/mcp-device-launch-smoke.mjs
//
// Uses com.android.settings (available on every Android device) so
// this works on any phone/emulator.
import { spawn } from "node:child_process";
import { once } from "node:events";
import path from "node:path";
import { fileURLToPath } from "node:url";

const serial = process.env.ATOMYX_ANDROID_SERIAL;
if (!serial) {
  console.error("ATOMYX_ANDROID_SERIAL required");
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
      try { responses.push(JSON.parse(line)); } catch {}
    }
  }
});

let nextId = 1;
function send(method, params = {}) {
  const id = nextId++;
  child.stdin.write(JSON.stringify({ jsonrpc: "2.0", id, method, params }) + "\n");
  return id;
}
function call(name, args = {}) {
  return send("tools/call", { name, arguments: args });
}
function waitFor(id, timeoutMs = 20000) {
  return new Promise((resolve, reject) => {
    const deadline = Date.now() + timeoutMs;
    const check = () => {
      const found = responses.find((r) => r.id === id);
      if (found) return resolve(found);
      if (Date.now() > deadline)
        return reject(new Error(`timeout id ${id}. stderr tail: ${stderrChunks.join("").slice(-600)}`));
      setTimeout(check, 50);
    };
    check();
  });
}
async function rpc(method, params) {
  const id = send(method, params);
  return waitFor(id);
}
async function callTool(name, args) {
  const id = call(name, args);
  const resp = await waitFor(id);
  return JSON.parse(resp.result?.content?.[0]?.text ?? "null");
}
function must(cond, msg) {
  if (!cond) {
    console.error("FAIL:", msg);
    child.kill("SIGKILL");
    process.exit(1);
  }
}

await new Promise((r) => setTimeout(r, 1500));
await rpc("initialize", {
  protocolVersion: "2024-11-05",
  capabilities: {},
  clientInfo: { name: "launch-smoke", version: "0" },
});
console.log("OK initialize");

// 1. launch Settings — exercises driver.launchApp → APK launch intent
const launched = await callTool("launch_app", { appId: "com.android.settings" });
must(launched?.ok === true, `launch_app: ${JSON.stringify(launched)}`);
console.log(`OK launch_app com.android.settings`);

// Settle for activity transition
await new Promise((r) => setTimeout(r, 1500));

// 2. get_ui_tree — real Settings screen
const tree = await callTool("get_ui_tree", { limit: 80 });
must(typeof tree?.total === "number" && tree.total > 0, "get_ui_tree total");
console.log(`OK get_ui_tree — total=${tree.total} returned=${tree.returned}`);

// 3. Find the first node with text — prove selector-by-text works on real screen
const sample = (tree.nodes ?? []).find((n) => n.text && n.text.length > 0);
if (sample) {
  const found = await callTool("find_element", { text: sample.text });
  must(found?.found === true, `find_element by text=${sample.text}: ${JSON.stringify(found)}`);
  console.log(`OK find_element text='${sample.text.slice(0, 40)}' center=${JSON.stringify(found.center)}`);
} else {
  console.log(`SKIP find_element — no text nodes in tree`);
}

// 4. start_run + report_bug + list_bugs round-trip on real device session
const run = await callTool("start_run", {
  name: "real-device-smoke",
  source: "automation",
});
must(run?.ok === true, "start_run");
console.log(`OK start_run id=${run.id}`);

const bug = await callTool("report_bug", {
  title: "Smoke evidence",
  description: "Device smoke test captured state of Settings screen",
  captureScreenshot: true,
});
must(bug?.ok === true, `report_bug: ${JSON.stringify(bug)}`);
console.log(`OK report_bug id=${bug.id} screenshotPath=${bug.screenshotPath ?? "(none)"}`);

const bugs = await callTool("list_bugs", { runId: run.id });
must(bugs?.count === 1, `list_bugs count expected 1, got ${bugs?.count}`);
console.log(`OK list_bugs — count=${bugs.count}`);

const getBug = await callTool("get_bug", { id: bug.id });
must(getBug?.ok === true && getBug.bug?.id === bug.id, "get_bug round-trip");
console.log(`OK get_bug title='${getBug.bug.title}'`);

const finish = await callTool("finish_run", {
  status: "passed",
  summary: "real device smoke test all green",
});
must(finish?.ok === true, "finish_run");
console.log(`OK finish_run — duration=${finish.durationMs}ms`);

// 5. press_key back — exercises Orchestra.pressKey → driver.pressKey
const back = await callTool("press_key", { key: "back" });
must(back?.ok === true, `press_key back: ${JSON.stringify(back)}`);
console.log(`OK press_key back`);

// Done
child.stdin.end();
child.kill("SIGTERM");
await once(child, "exit");
console.log("");
console.log("✓ MCP device launch-smoke passed");
