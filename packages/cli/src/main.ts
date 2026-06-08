#!/usr/bin/env node

import { createFsSkills } from "@atomyx/skills";
import { createRuntimeDriverFactory } from "./features/driver/index.js";
import { modules, shortcuts } from "./infra/router/router.js";

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

  const ctx = {
    driverFactory: createRuntimeDriverFactory(),
    skills: createFsSkills(),
  };
  const mods = modules(ctx);

  const command = args[0]!;
  const rest = args.slice(1);

  const shortcut = shortcuts[command];
  if (shortcut) {
    const resolved = shortcut(rest);
    const mod = mods[resolved.module];
    if (mod) {
      await mod.execute(resolved.args);
      return;
    }
  }

  const mod = mods[command];
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
