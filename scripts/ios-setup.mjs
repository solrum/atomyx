#!/usr/bin/env node
// `atomyx ios setup` — wraps the Xcode driver build steps so contributors
// don't have to memorize the incantation.
//
// Usage:
//   node scripts/ios-setup.mjs                  # defaults to booted simulator
//   node scripts/ios-setup.mjs --udid <UDID>    # specific simulator
//
// What it does:
//   1. Verify xcodegen + xcodebuild are installed
//   2. Run `xcodegen generate` in native/ios-driver/ to produce the .xcodeproj
//   3. Run `xcodebuild build-for-testing` against the target simulator
//
// It does NOT start the driver (that's a separate blocking command —
// see native/ios-driver/README.md).

import { spawn, spawnSync } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const DRIVER_DIR = path.resolve(__dirname, "../native/ios-driver");

function die(msg, code = 1) {
  console.error(`[ios-setup] ${msg}`);
  process.exit(code);
}

function parseArgs() {
  const args = process.argv.slice(2);
  const out = { udid: null };
  for (let i = 0; i < args.length; i++) {
    if (args[i] === "--udid" && i + 1 < args.length) {
      out.udid = args[++i];
    }
  }
  return out;
}

function requireCmd(name) {
  const r = spawnSync("which", [name], { encoding: "utf8" });
  if (r.status !== 0 || !r.stdout.trim()) {
    die(`\`${name}\` not found in PATH. Install it and retry.`);
  }
  return r.stdout.trim();
}

function detectBootedUdid() {
  const r = spawnSync("xcrun", ["simctl", "list", "-j", "devices", "booted"], { encoding: "utf8" });
  if (r.status !== 0) die(`xcrun simctl failed: ${r.stderr}`);
  try {
    const parsed = JSON.parse(r.stdout);
    for (const runtimeDevices of Object.values(parsed.devices ?? {})) {
      for (const d of runtimeDevices) {
        if (d.state === "Booted") return d.udid;
      }
    }
  } catch (e) {
    die(`failed to parse simctl output: ${e.message}`);
  }
  die("no booted simulator found. Boot one via Simulator.app or `xcrun simctl boot <UDID>` first.");
}

function runStreaming(cmd, args, cwd) {
  return new Promise((resolve, reject) => {
    const p = spawn(cmd, args, { cwd, stdio: "inherit" });
    p.on("close", (code) => (code === 0 ? resolve() : reject(new Error(`${cmd} exited ${code}`))));
  });
}

async function main() {
  const args = parseArgs();

  console.log("[ios-setup] checking prerequisites...");
  requireCmd("xcodegen");
  requireCmd("xcodebuild");
  requireCmd("xcrun");

  const udid = args.udid ?? detectBootedUdid();
  console.log(`[ios-setup] target simulator: ${udid}`);

  console.log("[ios-setup] generating Xcode project via xcodegen...");
  await runStreaming("xcodegen", ["generate"], DRIVER_DIR);

  console.log("[ios-setup] building driver for testing (first run takes ~1 minute)...");
  await runStreaming(
    "xcodebuild",
    [
      "build-for-testing",
      "-project", "AtomyxDriver.xcodeproj",
      "-scheme", "AtomyxDriver",
      "-destination", `platform=iOS Simulator,id=${udid}`,
      "-derivedDataPath", "./build",
      "-quiet",
    ],
    DRIVER_DIR,
  );

  console.log("[ios-setup] ✅ driver built. To start it:");
  console.log("");
  console.log(`  cd native/ios-driver`);
  console.log(`  SIMCTL_CHILD_ADET_SERVE=1 xcodebuild test-without-building \\`);
  console.log(`    -project AtomyxDriver.xcodeproj \\`);
  console.log(`    -scheme AtomyxDriver \\`);
  console.log(`    -destination 'platform=iOS Simulator,id=${udid}' \\`);
  console.log(`    -derivedDataPath ./build`);
  console.log("");
  console.log("  Wait for the log line:");
  console.log("    [atomyx] driver listening on 127.0.0.1:22087");
  console.log("");
  console.log("  Then in another terminal: node scripts/ios-driver-smoke.mjs");
}

main().catch((err) => {
  console.error(`[ios-setup] FAILED: ${err.message}`);
  process.exit(1);
});
