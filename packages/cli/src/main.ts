#!/usr/bin/env node
/**
 * Atomyx unified CLI — single entry point for all modules.
 *
 * Routes `atomyx <module> <command> …` to the module handler
 * registered in `router.ts`. Shortcut aliases (`atomyx run`,
 * `atomyx devices`) rewrite to their full form before dispatch.
 * New modules plug in by adding a `router.ts` entry.
 */

import { modules, shortcuts } from "./router.js";

const VERSION = "0.1.0";

async function main(): Promise<void> {
  const args = process.argv.slice(2);

  if (args.length === 0 || args[0] === "help" || args[0] === "--help" || args[0] === "-h") {
    printHelp();
    return;
  }

  if (args[0] === "version" || args[0] === "--version" || args[0] === "-v") {
    process.stdout.write(`atomyx ${VERSION}\n`);
    return;
  }

  const command = args[0]!;
  const rest = args.slice(1);

  // Check shortcuts first
  const shortcut = shortcuts[command];
  if (shortcut) {
    const resolved = shortcut(rest);
    const mod = modules[resolved.module];
    if (mod) {
      await mod.execute(resolved.args);
      return;
    }
  }

  // Check module commands
  const mod = modules[command];
  if (mod) {
    await mod.execute(rest);
    return;
  }

  process.stderr.write(`Unknown command: "${command}"\n\n`);
  printHelp();
  process.exit(2);
}

function printHelp(): void {
  const out = process.stdout.write.bind(process.stdout);
  out(`atomyx — Agentic Test Orchestration Module

USAGE
  atomyx <module> <command> [flags]

MODULES
  driver          Device interaction (run scripts, list devices)
  mcp             MCP stdio server (coming soon)
  test            Test management (coming soon)
  studio          Visual test IDE (coming soon)

SHORTCUTS
  atomyx run ...              → atomyx driver run ...
  atomyx devices              → atomyx driver list-devices

DRIVER COMMANDS
  atomyx driver run --file <path> --platform <ios|android> --device <id>
  atomyx driver list-devices [--json]

EXAMPLES
  atomyx run --file test.yml --platform ios --device <UDID>
  atomyx devices --json
  atomyx driver run --file test.yml --proxy mitmproxy:/tmp/capture.jsonl

VERSION
  atomyx version

SEE ALSO
  https://atomyx.dev
`);
}

main().catch((err) => {
  const message = err instanceof Error ? err.message : String(err);
  process.stderr.write(`error: ${message}\n`);
  if (process.env.ATOMYX_DEBUG === "1" && err instanceof Error && err.stack) {
    process.stderr.write(err.stack + "\n");
  }
  process.exit(1);
});
