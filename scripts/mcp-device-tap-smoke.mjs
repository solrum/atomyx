// Third-round device smoke: specifically exercises
// `Orchestra.tap(selector)` on real hardware, which goes through
// the full pipeline:
//
//   compileSelector → scroll.ensureVisible → detectObscurement →
//   coordinate tap
//
// This path was broken pre-batch-19 by a reference-identity
// mismatch in detectObscurement that reported "obscured by
// itself" on any interior leaf when run against a driver whose
// hierarchy() call returns fresh TreeNode instances (which every
// real driver does). The fix was to use the cursor's own tree
// root via `rootNodeOf(cursor)` instead of fetching a second
// `driver.hierarchy()`. This test proves the fix lands on real
// Android hardware.
//
// Flow: launch Settings, get_ui_tree, pick a clickable row with a
// stable resourceId or text, tap it, read the new tree and assert
// the screen changed. Navigate back. If the tap reports
// obscured-by-itself, batch 19 is not in this build.
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
function waitFor(id, timeoutMs = 20000) {
  return new Promise((resolve, reject) => {
    const deadline = Date.now() + timeoutMs;
    const check = () => {
      const found = responses.find((r) => r.id === id);
      if (found) return resolve(found);
      if (Date.now() > deadline)
        return reject(new Error(`timeout id ${id}`));
      setTimeout(check, 50);
    };
    check();
  });
}
async function callTool(name, args) {
  const id = send("tools/call", { name, arguments: args });
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
await waitFor(
  send("initialize", {
    protocolVersion: "2024-11-05",
    capabilities: {},
    clientInfo: { name: "tap-smoke", version: "0" },
  }),
);

// Launch Settings fresh so the test has a known starting state.
const launched = await callTool("launch_app", {
  appId: "com.android.settings",
});
must(launched?.ok === true, "launch_app");
console.log(`OK launch_app com.android.settings`);
await new Promise((r) => setTimeout(r, 1500));

// Read the initial tree.
const treeBefore = await callTool("get_ui_tree", { limit: 200 });
console.log(`OK get_ui_tree before — total=${treeBefore.total}`);

// Pick a clickable node in the BODY of the Settings screen — not
// an overlay (Samsung edge panel), not an action_bar element. We
// need a non-empty text field, a valid bounds parseable from the
// "l,t,r,b" string, and a center-y in the middle of the screen
// (roughly 300–1800 on a 1080×2400 device — avoids action bar at
// top and nav bar at bottom). Prefer a Settings-owned id so we
// don't accidentally target a Samsung overlay that happens to be
// drawn on top.
function parseBoundsStr(b) {
  if (!b) return null;
  const parts = b.split(",").map(Number);
  if (parts.length !== 4 || parts.some((n) => !Number.isFinite(n))) return null;
  return { left: parts[0], top: parts[1], right: parts[2], bottom: parts[3] };
}
// Real Android Settings renders rows as a RecyclerView where the
// clickable parent is an unnamed container and the visible text
// is on a child TextView (id="android:id/title") marked
// clickable=false. Android hit-testing propagates the tap to the
// parent click handler, so tapping the TextView's center still
// triggers the row — we just select by its visible text.
//
// Pick the first mid-screen title text that's NOT "Settings"
// (which is the collapsing app bar title — inert) and use it as
// the tap target. Orchestra.prepareSelectorForAction will scroll
// it into view, run the obscurement check, and dispatch a
// coordinate tap at its center. Android's click resolution does
// the rest.
const textNodes = (treeBefore.nodes ?? [])
  .filter((n) => n.text && n.text.length > 0)
  .map((n) => ({ node: n, bounds: parseBoundsStr(n.bounds) }))
  .filter((e) => e.bounds !== null);

const candidates = textNodes
  .filter((e) => {
    const cy = (e.bounds.top + e.bounds.bottom) / 2;
    return cy > 400 && cy < 2000;
  })
  .filter((e) => e.node.text !== "Settings")
  .filter((e) => (e.node.id ?? "").endsWith("/title"));
must(candidates.length > 0, "no title text found in Settings body");
const target = candidates[0].node;
console.log(
  `  chosen target: role=${target.role} text='${target.text}' id='${target.id ?? ""}' bounds='${target.bounds}'`,
);

// The actual tap. Prefer text selector (most agent-ergonomic). This is
// the batch 19 validation point: if obscurement reports "obscured by
// itself", we'd see ok:false + a reason string mentioning the target's
// own id/text/label.
const tapArgs = { text: target.text };
const tapResult = await callTool("tap", { selector: tapArgs });
must(
  tapResult?.ok === true,
  `tap selector=${JSON.stringify(tapArgs)} should succeed. Got: ${JSON.stringify(tapResult)}`,
);
console.log(
  `OK tap — selector=${JSON.stringify(tapArgs)} resolvedBy=${tapResult.resolvedBy}`,
);
console.log(`  → batch 19 obscurement fix validated on real hardware`);

// Wait for the tap to settle + new screen to load
await new Promise((r) => setTimeout(r, 1500));

// Read the tree again — it should have CHANGED. Use the target's
// title text as the "before" marker: if we tapped "Connections"
// and successfully navigated, "Connections" is likely promoted to
// the new action bar title while the old row list is gone. A
// structural change check (new texts appearing that weren't in
// the before snapshot) is a robust signal.
const treeAfter = await callTool("get_ui_tree", { limit: 200 });
const textsBefore = new Set(
  (treeBefore.nodes ?? []).map((n) => n.text).filter(Boolean),
);
const textsAfter = new Set(
  (treeAfter.nodes ?? []).map((n) => n.text).filter(Boolean),
);
const newTexts = [...textsAfter].filter((t) => !textsBefore.has(t));
console.log(
  `OK get_ui_tree after — total=${treeAfter.total} newTexts=${newTexts.length}`,
);
// Any significant tree change counts as "screen transitioned":
// either new texts appear, or the node count changes by a wide
// margin (e.g. navigation to a loading screen with fewer nodes).
const totalChange = Math.abs(treeAfter.total - treeBefore.total);
const changed = newTexts.length > 0 || totalChange >= 5;
must(
  changed,
  `screen did not navigate after tap on '${target.text}' (before=${treeBefore.total} after=${treeAfter.total} newTexts=${newTexts.length})`,
);
console.log(
  `  screen changed — total ${treeBefore.total} → ${treeAfter.total} (Δ=${totalChange}), newTexts=${newTexts.length}${newTexts.length ? `: ${newTexts.slice(0, 3).map((t) => `'${t.slice(0, 30)}'`).join(", ")}` : ""}`,
);

// Navigate back via press_key, landing us at Settings root again.
const back = await callTool("press_key", { key: "back" });
must(back?.ok === true, "press_key back");
console.log(`OK press_key back`);

child.stdin.end();
child.kill("SIGTERM");
await once(child, "exit");
console.log("");
console.log("✓ MCP device tap-smoke passed");
console.log("  real-device validation of batch 19 obscurement fix complete");
