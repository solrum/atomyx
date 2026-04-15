#!/usr/bin/env node
/**
 * adet CLI — standalone test spec runner for CI / local use.
 *
 * Usage:
 *   adet run tests/adet/login.yaml
 *   adet run "tests/adet/*.yaml" --device=auto --report=junit.xml
 *   adet explore --app=com.example --goal="find login bugs"
 *   adet list-devices
 */

import { connectDevice, listAllDevices } from "../adapters/device-router.js";
import { loadSpec, runSpec, type RunSummary } from "../runner/spec-runner.js";
import { createAdetContext } from "../runtime/adet-context.js";
import { writeJUnitXml } from "./junit-reporter.js";

interface Args {
  command: string;
  positional: string[];
  flags: Record<string, string | boolean>;
}

function parseArgs(argv: string[]): Args {
  const [command, ...rest] = argv;
  const positional: string[] = [];
  const flags: Record<string, string | boolean> = {};
  for (const arg of rest) {
    if (arg.startsWith("--")) {
      const eq = arg.indexOf("=");
      if (eq >= 0) flags[arg.slice(2, eq)] = arg.slice(eq + 1);
      else flags[arg.slice(2)] = true;
    } else {
      positional.push(arg);
    }
  }
  return { command, positional, flags };
}

const green = (s: string) => `\x1b[32m${s}\x1b[0m`;
const red = (s: string) => `\x1b[31m${s}\x1b[0m`;
const blue = (s: string) => `\x1b[34m${s}\x1b[0m`;
const dim = (s: string) => `\x1b[2m${s}\x1b[0m`;

async function expandPaths(patterns: string[]): Promise<string[]> {
  const fg = await import("node:fs").then((m) => m.promises);
  const path = await import("node:path");
  const out: string[] = [];
  for (const p of patterns) {
    if (p.includes("*")) {
      const dir = path.dirname(p);
      const pattern = path.basename(p);
      try {
        const entries = await fg.readdir(dir);
        const re = new RegExp("^" + pattern.replace(/\./g, "\\.").replace(/\*/g, ".*") + "$");
        for (const e of entries) {
          if (re.test(e)) out.push(path.join(dir, e));
        }
      } catch {}
    } else {
      out.push(p);
    }
  }
  return out;
}

async function pickDevice(deviceFlag: string | boolean | undefined): Promise<string> {
  const devices = await listAllDevices();
  if (devices.length === 0) {
    throw new Error("No devices found. Connect an Android device and run `adb devices` to verify.");
  }
  if (!deviceFlag || deviceFlag === "auto" || deviceFlag === true) {
    return devices[0].id;
  }
  if (typeof deviceFlag !== "string") {
    return devices[0].id;
  }
  const found = devices.find((d) => d.id === deviceFlag || d.serial === deviceFlag);
  if (!found) throw new Error(`Device not found: ${deviceFlag}`);
  return found.id;
}

async function cmdRun(args: Args) {
  const patterns = args.positional;
  if (patterns.length === 0) {
    console.error(red("usage: adet run <spec.yaml> [<spec.yaml> ...] [--device=<id>] [--report=junit.xml]"));
    process.exit(2);
  }

  const specPaths = await expandPaths(patterns);
  if (specPaths.length === 0) {
    console.error(red(`no specs matched: ${patterns.join(", ")}`));
    process.exit(2);
  }

  console.log(blue(`adet: running ${specPaths.length} spec(s)`));

  const deviceId = await pickDevice(args.flags.device);
  console.log(dim(`device: ${deviceId}`));

  const ctl = await connectDevice(deviceId);
  const ctx = createAdetContext();
  ctx.controller = ctl;
  const summaries: RunSummary[] = [];

  try {
    for (const path of specPaths) {
      console.log(blue(`\n→ ${path}`));
      try {
        const spec = loadSpec(path);
        const summary = await runSpec(ctx, ctl, spec, path);
        summaries.push(summary);
        const stamp = summary.status === "passed" ? green("✓") : red("✗");
        console.log(
          `${stamp} ${summary.spec} (${summary.steps.length} steps, ${summary.bugs} bugs, ${summary.durationMs}ms)`,
        );
        if (summary.status !== "passed") {
          for (const s of summary.steps.filter((s) => s.status === "failed")) {
            console.log(red(`  ✗ step ${s.index} (${s.kind}): ${s.error}`));
          }
        }
        if (summary.resultPath) console.log(dim(`  result: ${summary.resultPath}`));
      } catch (err) {
        console.log(red(`✗ load/run error: ${err instanceof Error ? err.message : String(err)}`));
        summaries.push({
          spec: path,
          status: "error",
          startedAt: Date.now(),
          finishedAt: Date.now(),
          durationMs: 0,
          steps: [],
          bugs: 0,
        });
      }
    }
  } finally {
    await ctl.dispose();
  }

  const passed = summaries.filter((s) => s.status === "passed").length;
  const failed = summaries.length - passed;

  console.log("");
  console.log(green(`  ${passed} passed`) + (failed > 0 ? "  " + red(`${failed} failed`) : ""));

  if (typeof args.flags.report === "string") {
    writeJUnitXml(summaries, args.flags.report);
    console.log(dim(`  junit: ${args.flags.report}`));
  }

  process.exit(failed > 0 ? 1 : 0);
}

async function cmdListDevices() {
  const devices = await listAllDevices();
  if (devices.length === 0) {
    console.log(dim("(no devices)"));
    return;
  }
  for (const d of devices) {
    console.log(`${d.id}\t${d.platform}\t${d.state}`);
  }
}

async function cmdExplore(args: Args) {
  // Loaded lazily so users without ANTHROPIC_API_KEY can still use `adet run`
  const { runExploration } = await import("../explorer/agent-loop.js");
  const goal = args.flags.goal as string;
  const app = args.flags.app as string;
  const maxSteps = args.flags["max-steps"] ? Number(args.flags["max-steps"]) : 30;
  if (!goal || !app) {
    console.error(red("usage: adet explore --app=<package> --goal=\"<description>\" [--max-steps=N]"));
    process.exit(2);
  }
  const deviceId = await pickDevice(args.flags.device);
  const ctl = await connectDevice(deviceId);
  try {
    const summary = await runExploration(ctl, { app, goal, maxSteps });
    console.log(JSON.stringify(summary, null, 2));
    process.exit(summary.bugs.length > 0 ? 1 : 0);
  } finally {
    await ctl.dispose();
  }
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  switch (args.command) {
    case "run":
      await cmdRun(args);
      break;
    case "list-devices":
      await cmdListDevices();
      break;
    case "explore":
      await cmdExplore(args);
      break;
    default:
      console.error("usage: adet <run|explore|list-devices> [args]");
      process.exit(2);
  }
}

main().catch((err) => {
  console.error(red(`✗ ${err instanceof Error ? err.message : String(err)}`));
  process.exit(1);
});
