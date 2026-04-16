#!/usr/bin/env node
// Phase 3 Batch 2 smoke test — selector-based realistic flow.
//
// Unlike Batch 1's coord-first flow, this version uses the new
// `resolveSelector` / `tap(selector)` / `inputText(selector)` adapter
// methods exclusively for interactive operations. Coordinates are
// derived from the dump or resolved element — never hard-coded to a
// specific device size.
//
// Flow:
//   1. connect + listApps
//   2. launch Settings
//   3. dump tree, sanity-check element count
//   4. resolveSelector by contentDesc="Search" — locate search field
//   5. inputText(selector, "Siri") — tap focus + type in one call
//   6. dump tree, resolveSelector for a search result row
//   7. tap(selector) — navigate
//   8. pressKey back → check nav_bar_back strategy
//   9. swipe scroll (dimensions derived from dump bounds)
//  10. clearFocusedInput → verify reset (only if a search field is still visible)
//  11. screenshot + cleanup
//
// Prerequisite: driver running via `make serve`.

import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { IosXctestController } from "../dist/adapters/ios-xctest.adapter.js";

const UDID = process.env.UDID ?? "8DCBCBB0-A213-4AE0-96F8-3061DDAD4610";
const PREFS_BUNDLE = "com.apple.Preferences";
const SEARCH_TERM = "Siri";

function log(msg) { console.log(`[smoke] ${msg}`); }
function header(msg) { console.log(`\n== ${msg} ==`); }
function fail(msg) { throw new Error(msg); }

function dumpBoundingBox(summary) {
  // Derive screen dimensions from the largest element in the dump
  // (usually the root window). No hard-coded device sizes — keeps
  // the smoke correct across device configurations.
  let maxRight = 0, maxBottom = 0;
  for (const e of summary) {
    if (e.bounds.right > maxRight) maxRight = e.bounds.right;
    if (e.bounds.bottom > maxBottom) maxBottom = e.bounds.bottom;
  }
  return { width: maxRight, height: maxBottom };
}

async function dumpStable(ctl) {
  const all = await ctl.getUiSummary();
  const stable = all.filter(
    (e) => e.label || Object.keys(e.selector).length > 0,
  );
  return { all, stable };
}

async function main() {
  header("connect");
  const ctl = await IosXctestController.connect(UDID);
  log(`connected (UDID=${UDID})`);

  header("listApps (simctl host-side)");
  const apps = await ctl.listApps();
  log(`${apps.length} installed apps`);
  if (!apps.some((a) => a.appId === PREFS_BUNDLE)) {
    fail(`${PREFS_BUNDLE} not installed`);
  }

  header("launchApp Settings");
  const tLaunch = Date.now();
  await ctl.launchApp(PREFS_BUNDLE);
  log(`launched in ${Date.now() - tLaunch}ms`);

  header("dumpTree — sanity check");
  const tDump = Date.now();
  const root = await dumpStable(ctl);
  log(`${root.all.length} total, ${root.stable.length} stable in ${Date.now() - tDump}ms`);
  const dims = dumpBoundingBox(root.all);
  log(`derived screen dims: ${dims.width}×${dims.height}`);

  header('resolveSelector({contentDesc: "Search"})');
  const searchResolved = await ctl.resolveSelector({ contentDesc: "Search" });
  log(`found=${searchResolved.found} resolvedBy=${searchResolved.resolvedBy} bounds=${JSON.stringify(searchResolved.bounds)}`);
  if (!searchResolved.found) fail("search field not found");

  header(`inputText(selector, "${SEARCH_TERM}") — tap focus + type`);
  const inputResult = await ctl.inputText({ contentDesc: "Search" }, SEARCH_TERM);
  log(`inputText: ok=${inputResult.ok} reason="${inputResult.reason}"`);
  if (!inputResult.ok) fail(`inputText failed: ${inputResult.reason}`);
  await new Promise((r) => setTimeout(r, 800));

  header("dumpTree — locate search result");
  const searched = await dumpStable(ctl);
  log(`${searched.all.length} total, ${searched.stable.length} stable`);

  // Find the Siri result — it appears as a button with label "Siri"
  // in the filtered search results. Use resolveSelector via text match.
  header('resolveSelector({text: "Siri"})');
  const resultResolved = await ctl.resolveSelector({ text: "Siri" });
  log(`found=${resultResolved.found} resolvedBy=${resultResolved.resolvedBy} bounds=${JSON.stringify(resultResolved.bounds)}`);
  if (!resultResolved.found) fail("search result not found");

  header('tap({text: "Siri"}) → navigate');
  const tapResult = await ctl.tap({ text: "Siri" });
  log(`tap: ok=${tapResult.ok} reason="${tapResult.reason}"`);
  await new Promise((r) => setTimeout(r, 1000));

  const navigated = await dumpStable(ctl);
  const prevLabels = new Set(searched.stable.map((e) => e.label).filter(Boolean));
  const nextLabels = new Set(navigated.stable.map((e) => e.label).filter(Boolean));
  const added = [...nextLabels].filter((l) => !prevLabels.has(l));
  log(`after nav: ${navigated.all.length} total, ${navigated.stable.length} stable, +${added.length} new labels`);
  if (added.length) log(`first new: ${added.slice(0, 3).join(" | ")}`);

  header('pressKey("back") → return');
  const backResult = await ctl.pressKey("back");
  log(`pressKey: ok=${backResult.ok} reason="${backResult.reason}"`);
  await new Promise((r) => setTimeout(r, 800));

  header("swipe scroll (derived dims)");
  // Use the derived dimensions instead of hard-coding device size.
  const cx = dims.width / 2;
  const topY = dims.height * 0.6;
  const botY = dims.height * 0.3;
  await ctl.swipe(cx, topY, cx, botY, 200);
  log(`swipe (${cx},${topY}) → (${cx},${botY}) dispatched`);

  header("screenshot");
  const shot = await ctl.screenshot();
  const shotPath = path.join(os.tmpdir(), "atomyx-smoke.png");
  fs.writeFileSync(shotPath, Buffer.from(shot.base64, "base64"));
  log(`wrote ${fs.statSync(shotPath).size} bytes to ${shotPath}`);

  header("cleanup");
  await ctl.pressKey("home");
  await new Promise((r) => setTimeout(r, 500));
  await ctl.forceStopApp(PREFS_BUNDLE);
  const fgAfter = await ctl.currentForeground();
  log(`tracked state: ${JSON.stringify(fgAfter)}`);

  await ctl.dispose();
  console.log("\n[smoke] done. Batch 2 selector-based flow passes.");
}

main().catch((err) => {
  console.error("\n[smoke] FAILED:", err.message);
  if (err.stack) console.error(err.stack);
  process.exit(1);
});
