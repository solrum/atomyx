// End-to-end smoke test of the MCP stdio transport layer.
//
// Spawns `mcp-stdio-smoke-subprocess.mjs` as a child, connects to
// its stdin/stdout, sends a sequence of JSON-RPC requests, and
// asserts the responses. Run from the repo root:
//
//   node scripts/mcp-stdio-smoke.mjs
//
// Different from the unit-test suite in `packages/core-driver-mcp/`
// which drives tools/* via the server's internal handler map —
// this smoke test actually goes through the StdioServerTransport
// line-delimited JSON-RPC framing, which is the path real MCP
// clients use. Proves the binary layer is wired correctly.
import { spawn } from "node:child_process";
import { once } from "node:events";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const subprocessPath = path.join(here, "mcp-stdio-smoke-subprocess.mjs");

const child = spawn(process.execPath, [subprocessPath], {
  stdio: ["pipe", "pipe", "pipe"],
});

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
    if (line.trim()) responses.push(JSON.parse(line));
  }
});

function send(req) {
  child.stdin.write(JSON.stringify(req) + "\n");
}

function waitFor(id, timeoutMs = 3000) {
  return new Promise((resolve, reject) => {
    const deadline = Date.now() + timeoutMs;
    const check = () => {
      const found = responses.find((r) => r.id === id);
      if (found) return resolve(found);
      if (Date.now() > deadline)
        return reject(
          new Error(
            `timeout waiting for id ${id}. stderr: ${stderrChunks.join("").slice(0, 500)}`,
          ),
        );
      setTimeout(check, 10);
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

// Let the child start.
await new Promise((r) => setTimeout(r, 300));

// 1. MCP initialize
send({
  jsonrpc: "2.0",
  id: 1,
  method: "initialize",
  params: {
    protocolVersion: "2024-11-05",
    capabilities: {},
    clientInfo: { name: "smoke", version: "0" },
  },
});
const initResp = await waitFor(1);
must(initResp.result?.serverInfo?.name === "atomyx", "initialize server info");
console.log(`OK initialize — server=${initResp.result.serverInfo.name}@${initResp.result.serverInfo.version}`);

// 2. tools/list
send({ jsonrpc: "2.0", id: 2, method: "tools/list", params: {} });
const listResp = await waitFor(2);
const tools = listResp.result?.tools ?? [];
must(tools.length === 26, `expected 26 tools, got ${tools.length}`);
console.log(`OK tools/list — ${tools.length} tools registered`);

// 3. prompts/list (methodology prompts shipped in batch 8)
send({ jsonrpc: "2.0", id: 3, method: "prompts/list", params: {} });
const promptsResp = await waitFor(3);
const prompts = promptsResp.result?.prompts ?? [];
must(prompts.length >= 4, `expected at least 4 prompts, got ${prompts.length}`);
console.log(`OK prompts/list — ${prompts.length} prompts registered`);

// 4. get_ui_tree — proves Orchestra → MockDriver.hierarchy works
send({
  jsonrpc: "2.0",
  id: 4,
  method: "tools/call",
  params: { name: "get_ui_tree", arguments: {} },
});
const treeResp = await waitFor(4);
const treeResult = JSON.parse(treeResp.result?.content?.[0]?.text ?? "null");
must(treeResult?.total === 3, `expected 3 nodes, got ${treeResult?.total}`);
console.log(`OK get_ui_tree — total=${treeResult.total} returned=${treeResult.returned}`);

// 5. find_element — proves selector pipeline + priority broadening
send({
  jsonrpc: "2.0",
  id: 5,
  method: "tools/call",
  params: { name: "find_element", arguments: { id: "login_btn" } },
});
const findResp = await waitFor(5);
const findResult = JSON.parse(findResp.result?.content?.[0]?.text ?? "null");
must(findResult?.found === true, "find_element should find login_btn");
must(findResult?.center?.x === 215, `expected center.x=215, got ${findResult?.center?.x}`);
console.log(`OK find_element — center=${JSON.stringify(findResult.center)}`);

// 6. tap with selector — proves the FULL pipeline: scroll-into-view +
// obscurement check (the exact path the batch 19 bug broke on real
// devices). Passes only because batch 19's rootNodeOf fix is in.
send({
  jsonrpc: "2.0",
  id: 6,
  method: "tools/call",
  params: { name: "tap", arguments: { selector: { id: "login_btn" } } },
});
const tapResp = await waitFor(6);
const tapResult = JSON.parse(tapResp.result?.content?.[0]?.text ?? "null");
must(tapResult?.ok === true, `tap should succeed, got: ${JSON.stringify(tapResult)}`);
must(tapResult?.resolvedBy === "id", `expected resolvedBy=id, got ${tapResult?.resolvedBy}`);
console.log(`OK tap — ok=${tapResult.ok} resolvedBy=${tapResult.resolvedBy}`);

// 7. start_run + report_bug + list_bugs + get_bug + delete_bug —
// end-to-end run lifecycle + persistence round-trip.
send({
  jsonrpc: "2.0",
  id: 7,
  method: "tools/call",
  params: { name: "start_run", arguments: { name: "smoke-session" } },
});
const startResp = await waitFor(7);
const startResult = JSON.parse(startResp.result?.content?.[0]?.text ?? "null");
must(startResult?.ok === true, "start_run");
console.log(`OK start_run — id=${startResult.id}`);

send({
  jsonrpc: "2.0",
  id: 8,
  method: "tools/call",
  params: {
    name: "report_bug",
    arguments: {
      title: "smoke-detected issue",
      description: "exercising the persistence path end-to-end",
      captureScreenshot: false,
    },
  },
});
const bugResp = await waitFor(8);
const bugResult = JSON.parse(bugResp.result?.content?.[0]?.text ?? "null");
must(bugResult?.ok === true, "report_bug");
console.log(`OK report_bug — id=${bugResult.id}`);

send({
  jsonrpc: "2.0",
  id: 9,
  method: "tools/call",
  params: { name: "list_bugs", arguments: {} },
});
const listBugsResp = await waitFor(9);
const listBugsResult = JSON.parse(listBugsResp.result?.content?.[0]?.text ?? "null");
must(listBugsResult?.count === 1, `expected 1 bug, got ${listBugsResult?.count}`);
console.log(`OK list_bugs — count=${listBugsResult.count}`);

send({
  jsonrpc: "2.0",
  id: 10,
  method: "tools/call",
  params: { name: "delete_bug", arguments: { id: bugResult.id } },
});
const deleteResp = await waitFor(10);
const deleteResult = JSON.parse(deleteResp.result?.content?.[0]?.text ?? "null");
must(deleteResult?.ok === true, "delete_bug");
console.log(`OK delete_bug — id=${deleteResult.id}`);

// Clean up.
child.kill("SIGTERM");
await once(child, "exit");

console.log("");
console.log("✓ MCP stdio smoke test passed");
console.log("  10/10 JSON-RPC round-trips succeeded via real StdioServerTransport framing");
