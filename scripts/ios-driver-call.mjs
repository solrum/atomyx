#!/usr/bin/env node
// Minimal CLI wrapper for the running iOS driver. Send one command,
// pretty-print the response, exit. Designed for interactive
// discovery: user drives the simulator manually (tap with mouse to
// focus fields, navigate screens), then runs this to snapshot state.
//
// Prerequisite: driver running via `make serve`.
//
// Usage:
//   node scripts/ios-driver-call.mjs <type> [args-json]
//
// Examples:
//   # Dump current tree
//   node scripts/ios-driver-call.mjs dumpTree
//
//   # Dump only 50 elements
//   node scripts/ios-driver-call.mjs dumpTree '{"limit":50}'
//
//   # Keyboard state
//   node scripts/ios-driver-call.mjs getKeyboard
//
//   # Resolve a selector
//   node scripts/ios-driver-call.mjs resolveSelector '{"resourceId":"A01-01-01/6"}'
//
//   # Launch a bundle
//   node scripts/ios-driver-call.mjs launchApp '{"bundleId":"inc.guide.kabuappStation.dev"}'
//
//   # Tap coordinates
//   node scripts/ios-driver-call.mjs tapAt '{"x":200,"y":400}'
//
// Flags (pass BEFORE <type>):
//   --stable          filter dumpTree response to stable elements only
//   --keys-only       filter getKeyboard response to just the keys array
//   --port <port>     override default 22087

import net from "node:net";

const argv = process.argv.slice(2);
let port = 22087;
let stableOnly = false;
let keysOnly = false;

// Parse flags
while (argv.length && argv[0].startsWith("--")) {
  const flag = argv.shift();
  if (flag === "--stable") stableOnly = true;
  else if (flag === "--keys-only") keysOnly = true;
  else if (flag === "--port") port = parseInt(argv.shift(), 10);
  else {
    console.error(`unknown flag: ${flag}`);
    process.exit(1);
  }
}

const [cmdType, argsJson = "{}"] = argv;
if (!cmdType) {
  console.error("usage: ios-driver-call [flags] <type> [args-json]");
  console.error("see script header for examples");
  process.exit(1);
}

let args;
try {
  args = JSON.parse(argsJson);
} catch (e) {
  console.error(`bad args JSON: ${e.message}`);
  process.exit(1);
}

const req = JSON.stringify({ id: 1, type: cmdType, args }) + "\n";

const sock = net.createConnection({ host: "127.0.0.1", port });
sock.setEncoding("utf8");
let buf = "";

sock.on("connect", () => {
  sock.write(req);
});

sock.on("data", (chunk) => {
  buf += chunk;
  const nl = buf.indexOf("\n");
  if (nl < 0) return;
  const line = buf.slice(0, nl);

  let msg;
  try {
    msg = JSON.parse(line);
  } catch {
    console.log(line);
    sock.end();
    return;
  }

  if (!msg.ok) {
    console.error(`ERROR: ${msg.error ?? "unknown"}`);
    sock.end();
    process.exit(1);
  }

  // Apply output filters
  if (stableOnly && cmdType === "dumpTree" && Array.isArray(msg.data?.elements)) {
    const stable = msg.data.elements.filter(
      (e) => e.id || e.label || (e.value && e.value.length > 0),
    );
    console.log(`total=${msg.data.total} count=${msg.data.count} stable=${stable.length}`);
    for (const el of stable) {
      const bits = [`role=${(el.type || "other").padEnd(14)}`];
      if (el.id) bits.push(`id="${el.id}"`);
      if (el.label) bits.push(`label="${el.label}"`);
      if (el.value) bits.push(`value="${el.value}"`);
      bits.push(`bounds=${el.x - Math.floor(el.w / 2)},${el.y - Math.floor(el.h / 2)}..${el.x + Math.ceil(el.w / 2)},${el.y + Math.ceil(el.h / 2)}`);
      console.log(`  ${bits.join(" ")}`);
    }
  } else if (keysOnly && cmdType === "getKeyboard" && msg.data?.visible) {
    console.log(`visible=true layout=${msg.data.layout} bounds=${JSON.stringify(msg.data.bounds)}`);
    console.log(`${msg.data.keys.length} keys:`);
    for (const k of msg.data.keys) {
      console.log(`  label="${k.label}" bounds=${JSON.stringify(k.bounds)}`);
    }
  } else {
    console.log(JSON.stringify(msg.data ?? msg, null, 2));
  }

  sock.end();
});

sock.on("error", (err) => {
  console.error(`connect error: ${err.message}`);
  console.error("is the driver running? start with: cd native/ios-driver && make serve");
  process.exit(2);
});
