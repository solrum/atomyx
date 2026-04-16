#!/usr/bin/env node
// Flutter custom-keyboard probe — discovers the shape of in-app
// keypads so we can design a per-key-tap fallback for typeText.
//
// Flow:
//   1. launch inc.guide.kabuappStation.dev
//   2. dumpTree once (pre-focus) — print layout
//   3. resolveSelector for a text field by role
//   4. tap the field to trigger the keypad
//   5. getKeyboard — check if iOS system keyboard is visible
//      (expected: NOT visible, because custom Flutter keypad)
//   6. dumpTree again (post-focus) — print elements that look like
//      keys: short label (1–2 chars), interactive-ish, bottom half
//
// Output guides the custom-keyboard fallback design in Batch 3b.
//
// Usage: node scripts/ios-flutter-probe.mjs
// Env: UDID=<simulator-udid> to override target

import { IosXctestController } from "../dist/adapters/ios-xctest.adapter.js";

const UDID = process.env.UDID ?? "8DCBCBB0-A213-4AE0-96F8-3061DDAD4610";
const FLUTTER_BUNDLE = process.env.BUNDLE ?? "inc.guide.kabuappStation.dev";

function log(msg) { console.log(`[probe] ${msg}`); }
function header(msg) { console.log(`\n== ${msg} ==`); }

function fmt(el) {
  const sel = Object.keys(el.selector).length ? JSON.stringify(el.selector) : "{}";
  const b = el.bounds;
  return `role=${el.role.padEnd(14)} label="${el.label}" ${b.left},${b.top}..${b.right},${b.bottom} ${sel}`;
}

function dumpBoundingBox(summary) {
  let maxRight = 0, maxBottom = 0;
  for (const e of summary) {
    if (e.bounds.right > maxRight) maxRight = e.bounds.right;
    if (e.bounds.bottom > maxBottom) maxBottom = e.bounds.bottom;
  }
  return { width: maxRight, height: maxBottom };
}

async function main() {
  header("connect");
  const ctl = await IosXctestController.connect(UDID);
  log("connected");

  header("check installed");
  const apps = await ctl.listApps();
  const found = apps.find((a) => a.appId === FLUTTER_BUNDLE);
  if (!found) {
    console.error(`[probe] FAIL: ${FLUTTER_BUNDLE} NOT installed on ${UDID}`);
    console.error(`[probe] install via:`);
    console.error(`  xcrun simctl install ${UDID} <path-to-app.app>`);
    console.error(`  or drag .app bundle onto the Simulator window`);
    console.error(`[probe] or override with BUNDLE=<other-bundle-id> env var`);
    await ctl.dispose();
    process.exit(1);
  }
  log(`found: ${found.appId} (${found.label ?? "no label"})`);

  header(`launchApp ${FLUTTER_BUNDLE}`);
  try {
    await ctl.launchApp(FLUTTER_BUNDLE);
    log("launched");
  } catch (err) {
    console.error(`[probe] launch failed: ${err.message}`);
    await ctl.dispose();
    process.exit(1);
  }
  await new Promise((r) => setTimeout(r, 1500)); // let first screen settle

  header("PRE-focus tree dump");
  let summary = await ctl.getUiSummary();
  const dims = dumpBoundingBox(summary);
  log(`${summary.length} total elements, derived dims ${dims.width}×${dims.height}`);
  const stable = summary.filter(
    (e) => e.label || Object.keys(e.selector).length > 0,
  );
  log(`${stable.length} stable elements:`);
  for (const el of stable.slice(0, 30)) {
    console.log(`  ${fmt(el)}`);
  }
  if (stable.length > 30) log(`  ... +${stable.length - 30} more`);

  // Find a text field candidate — Flutter apps typically surface
  // input fields as textField/secureTextField OR as generic buttons/
  // cells. Try several strategies.
  header("locate input field");
  const inputCandidates = stable.filter(
    (e) =>
      e.role === "textField" ||
      e.role === "secureTextField" ||
      (typeof e.label === "string" &&
        /account|number|password|id|ログイン|口座|番号/i.test(e.label)),
  );
  log(`${inputCandidates.length} input candidates:`);
  for (const el of inputCandidates.slice(0, 10)) {
    console.log(`  ${fmt(el)}`);
  }
  if (inputCandidates.length === 0) {
    log("WARN: no input-like element found. Printing all non-image stable elements:");
    for (const el of stable.filter((e) => e.role !== "image").slice(0, 20)) {
      console.log(`  ${fmt(el)}`);
    }
    log("probe stopping here — manual inspection needed to locate input");
    await ctl.forceStopApp(FLUTTER_BUNDLE);
    await ctl.dispose();
    return;
  }

  const target = inputCandidates[0];
  const tx = (target.bounds.left + target.bounds.right) / 2;
  const ty = (target.bounds.top + target.bounds.bottom) / 2;
  log(`tapping input candidate at (${tx}, ${ty}): ${target.label || target.selector.resourceId || "no label"}`);
  await ctl.tapCoordinates(tx, ty);
  await new Promise((r) => setTimeout(r, 1200)); // keyboard animation

  header("getKeyboard (post-focus)");
  const kb = await ctl.getKeyboard();
  log(`visible=${kb.visible} layout=${kb.layout} keys=${kb.keys.length}`);
  if (kb.visible) {
    log("WARN: iOS system keyboard IS visible — this field uses UIKeyboard, not custom Flutter widget");
    log("probe stopping — native typeText path should work for this field");
    await ctl.forceStopApp(FLUTTER_BUNDLE);
    await ctl.dispose();
    return;
  }
  log("confirmed: no system keyboard → custom in-app keypad expected");

  header("POST-focus tree dump — searching for custom keypad");
  summary = await ctl.getUiSummary();
  log(`${summary.length} total elements (delta ${summary.length - stable.length} from pre-focus stable)`);

  // Filter: short label (1-3 chars), bottom half of screen, any role
  const bottomHalf = dims.height / 2;
  const keyLike = summary.filter(
    (e) =>
      typeof e.label === "string" &&
      e.label.length >= 1 &&
      e.label.length <= 3 &&
      e.bounds.top >= bottomHalf,
  );
  log(`${keyLike.length} key-like elements (short label + bottom half):`);
  for (const el of keyLike) {
    console.log(`  ${fmt(el)}`);
  }

  // Separate analysis: digit-only labels (numeric keypad candidates)
  const digitKeys = keyLike.filter((e) => /^[0-9]$/.test(e.label));
  header(`digit key candidates (${digitKeys.length})`);
  for (const el of digitKeys) {
    console.log(`  ${fmt(el)}`);
  }
  if (digitKeys.length >= 9) {
    log(`✅ looks like a numeric keypad — ${digitKeys.length} digit buttons found`);
    log("fallback path viable: map digit → bounds, tap midpoint per char");
  } else {
    log(`⚠️ only ${digitKeys.length} digit labels — keypad shape may differ`);
  }

  // Sample the grid layout — for digit keys, compute bounding box
  if (digitKeys.length > 0) {
    const xs = digitKeys.flatMap((e) => [e.bounds.left, e.bounds.right]);
    const ys = digitKeys.flatMap((e) => [e.bounds.top, e.bounds.bottom]);
    const keypadBox = {
      left: Math.min(...xs),
      top: Math.min(...ys),
      right: Math.max(...xs),
      bottom: Math.max(...ys),
    };
    log(`keypad bounding box: ${keypadBox.left},${keypadBox.top}..${keypadBox.right},${keypadBox.bottom}`);
  }

  await ctl.forceStopApp(FLUTTER_BUNDLE);
  await ctl.dispose();
  log("done");
}

main().catch((err) => {
  console.error("[probe] ERROR:", err.message);
  if (err.stack) console.error(err.stack);
  process.exit(1);
});
