#!/usr/bin/env node
// Diagnostic probe: discover which tap mechanism triggers Flutter's
// TextField focus on the target app. Tries 4 variants in sequence,
// screenshot-diffs between each to detect state change.
//
// If NONE work → Flutter gesture system is rejecting all synthetic
// taps → need a different approach (e.g. typeText on element without
// focus, which bypasses keypad entirely).
//
// Usage: node scripts/ios-flutter-tap-probe.mjs
// Env: UDID, BUNDLE

import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { IosXctestController } from "../dist/adapters/ios-xctest.adapter.js";

const UDID = process.env.UDID ?? "8DCBCBB0-A213-4AE0-96F8-3061DDAD4610";
const BUNDLE = process.env.BUNDLE ?? "inc.guide.kabuappStation.dev";

function log(msg) { console.log(`[tap-probe] ${msg}`); }
function header(msg) { console.log(`\n== ${msg} ==`); }

async function saveShot(ctl, name) {
  const shot = await ctl.screenshot();
  const bytes = Buffer.from(shot.base64, "base64");
  const p = path.join(os.tmpdir(), `atomyx-tap-probe-${name}.png`);
  fs.writeFileSync(p, bytes);
  return { path: p, size: bytes.length };
}

async function treeSignature(ctl) {
  const summary = await ctl.getUiSummary();
  // Hash-ish: count + concatenated labels of first N stable elements
  const stable = summary.filter(
    (e) => e.label || Object.keys(e.selector).length > 0,
  );
  const labels = stable.slice(0, 30).map((e) => e.label || "").join("|");
  return { count: summary.length, stable: stable.length, sig: labels };
}

async function main() {
  const ctl = await IosXctestController.connect(UDID);
  log(`connected to ${UDID}`);

  await ctl.launchApp(BUNDLE);
  await new Promise((r) => setTimeout(r, 2000));
  log(`launched ${BUNDLE}`);

  // Locate target field via the resourceId found in earlier probe.
  // A01-01-01/6 = account number field on kabuappStation login screen.
  const targetId = process.env.TARGET_ID ?? "A01-01-01/6";
  header(`resolve target ${targetId}`);
  const resolved = await ctl.resolveSelector({ resourceId: targetId });
  if (!resolved.found) {
    log(`FAIL: ${targetId} not found — is the app on the login screen?`);
    await ctl.dispose();
    process.exit(1);
  }
  const cx = (resolved.bounds.left + resolved.bounds.right) / 2;
  const cy = (resolved.bounds.top + resolved.bounds.bottom) / 2;
  log(`target bounds=${JSON.stringify(resolved.bounds)} midpoint=(${cx},${cy})`);

  header("baseline");
  const baselineShot = await saveShot(ctl, "0-baseline");
  const baselineTree = await treeSignature(ctl);
  log(`baseline: ${baselineTree.count} elems, ${baselineTree.stable} stable, screenshot ${baselineShot.size} bytes → ${baselineShot.path}`);
  const baselineSig = baselineTree.sig;

  // Variant 1: single tapCoordinates
  header("variant 1: tapCoordinates");
  await ctl.tapCoordinates(cx, cy);
  await new Promise((r) => setTimeout(r, 1500));
  let tree = await treeSignature(ctl);
  let shot = await saveShot(ctl, "1-tapAt");
  let treeChanged = tree.sig !== baselineSig;
  let pixelChanged = Math.abs(shot.size - baselineShot.size) > 500;
  log(`tree ${tree.count}/${tree.stable} changed=${treeChanged} px-delta=${Math.abs(shot.size - baselineShot.size)} pxChanged=${pixelChanged}`);
  if (treeChanged || pixelChanged) {
    log(`✅ variant 1 WORKED — tapCoordinates triggers Flutter focus`);
    await cleanup(ctl);
    return;
  }

  // Variant 2: longPressAt 100ms
  header("variant 2: longPressAt 100ms");
  await ctl.longPressCoordinates(cx, cy, 100);
  await new Promise((r) => setTimeout(r, 1500));
  tree = await treeSignature(ctl);
  shot = await saveShot(ctl, "2-longPress100");
  treeChanged = tree.sig !== baselineSig;
  pixelChanged = Math.abs(shot.size - baselineShot.size) > 500;
  log(`tree ${tree.count}/${tree.stable} changed=${treeChanged} px-delta=${Math.abs(shot.size - baselineShot.size)} pxChanged=${pixelChanged}`);
  if (treeChanged || pixelChanged) {
    log(`✅ variant 2 WORKED — longPressAt 100ms triggers Flutter focus`);
    await cleanup(ctl);
    return;
  }

  // Variant 3: double tap (two rapid tapCoordinates)
  header("variant 3: double tap (100ms gap)");
  await ctl.tapCoordinates(cx, cy);
  await new Promise((r) => setTimeout(r, 100));
  await ctl.tapCoordinates(cx, cy);
  await new Promise((r) => setTimeout(r, 1500));
  tree = await treeSignature(ctl);
  shot = await saveShot(ctl, "3-doubleTap");
  treeChanged = tree.sig !== baselineSig;
  pixelChanged = Math.abs(shot.size - baselineShot.size) > 500;
  log(`tree ${tree.count}/${tree.stable} changed=${treeChanged} px-delta=${Math.abs(shot.size - baselineShot.size)} pxChanged=${pixelChanged}`);
  if (treeChanged || pixelChanged) {
    log(`✅ variant 3 WORKED — double-tap triggers Flutter focus`);
    await cleanup(ctl);
    return;
  }

  // Variant 4: tap via selector (adapter composes resolve+tap)
  header("variant 4: tap(selector)");
  const tapResult = await ctl.tap({ resourceId: targetId });
  log(`tap result: ${JSON.stringify(tapResult)}`);
  await new Promise((r) => setTimeout(r, 1500));
  tree = await treeSignature(ctl);
  shot = await saveShot(ctl, "4-tapSelector");
  treeChanged = tree.sig !== baselineSig;
  pixelChanged = Math.abs(shot.size - baselineShot.size) > 500;
  log(`tree ${tree.count}/${tree.stable} changed=${treeChanged} px-delta=${Math.abs(shot.size - baselineShot.size)} pxChanged=${pixelChanged}`);
  if (treeChanged || pixelChanged) {
    log(`✅ variant 4 WORKED`);
    await cleanup(ctl);
    return;
  }

  header("ALL VARIANTS FAILED");
  log("no tap mechanism detected a state change");
  log("possible causes:");
  log("  1. Flutter keypad IS visible but not in a11y tree (check simulator visually)");
  log("  2. Field is already focused and the keypad is always there (check baseline screenshot)");
  log("  3. Flutter rejects synthetic touch events entirely");
  log("");
  log("compare screenshots side-by-side:");
  log(`  open ${path.join(os.tmpdir(), "atomyx-tap-probe-0-baseline.png")}`);
  log(`  open ${path.join(os.tmpdir(), "atomyx-tap-probe-1-tapAt.png")}`);
  log(`  open ${path.join(os.tmpdir(), "atomyx-tap-probe-4-tapSelector.png")}`);
  log("if any of them shows the keypad visually → need coord-based hardcoded");
  log("fallback; a11y-based fallback is not viable for this app");

  await cleanup(ctl);
}

async function cleanup(ctl) {
  try {
    await ctl.forceStopApp(BUNDLE);
  } catch {}
  await ctl.dispose();
}

main().catch((err) => {
  console.error("[tap-probe] ERROR:", err.message);
  if (err.stack) console.error(err.stack);
  process.exit(1);
});
