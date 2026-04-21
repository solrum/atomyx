import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { Orchestra } from "@atomyx/driver/orchestra";
import { SystemClock, ConsoleLogger } from "@atomyx/core/infra";
import { AndroidDriver } from "@atomyx/android-driver";
import { IosDriver } from "@atomyx/ios-driver";
import type { Driver } from "@atomyx/driver/driver";
import type { CaptureConfig } from "@atomyx/shared/script";
import {
  parseScript,
  ScriptRunner,
  createCapture,
} from "@atomyx/script";

/**
 * `atomyx-driver run` — execute a YML test script against a
 * real device. Wires up Driver + Orchestra + ScriptRunner
 * and prints results.
 *
 * Required: --file <path>
 * Optional: --platform, --device, --proxy, --json
 */
export async function runScript(
  flags: Readonly<Record<string, string | boolean>>,
): Promise<void> {
  const filePath = flags["--file"];
  if (typeof filePath !== "string") {
    process.stderr.write(
      "error: --file <path> is required for the run command.\n",
    );
    process.exit(2);
  }

  const platform = (flags["--platform"] as string) ?? "android";
  const deviceId = flags["--device"] as string | undefined;
  const proxyFlag = flags["--proxy"] as string | undefined;
  const jsonOutput = !!flags["--json"];

  const absPath = resolve(filePath);
  const yamlContent = readFileSync(absPath, "utf-8");
  const script = parseScript(yamlContent);

  const driver = createDriver(platform, deviceId);
  const clock = new SystemClock();
  const logger = new ConsoleLogger();

  const captureConfig = parseCaptureFlag(proxyFlag);
  const networkCapture = createCapture(captureConfig);

  try {
    await driver.connect();
    await networkCapture.start();

    const orchestra = new Orchestra({ driver, clock, logger });
    const runner = new ScriptRunner({
      orchestra,
      clock,
      logger,
      networkCapture,
    });

    const result = await runner.run(script);

    await networkCapture.stop();
    await driver.disconnect();

    if (jsonOutput) {
      // Replace screenshot buffers with size-only metadata so the
      // JSON surface stays a reasonable size regardless of how
      // many screenshots the script captured.
      const output = {
        ...result,
        artifacts: {
          screenshots: result.artifacts
            .getScreenshots()
            .map((s) => ({ label: s.label, sizeBytes: s.data.length })),
        },
      };
      process.stdout.write(JSON.stringify(output, null, 2) + "\n");
    } else {
      printTextResult(result);
    }

    process.exit(result.ok ? 0 : 1);
  } catch (err) {
    await networkCapture.stop().catch(() => {});
    await driver.disconnect().catch(() => {});
    throw err;
  }
}

// Default ADB serial assigned by Android Studio / emulator CLI
// when a single emulator boots. Used only when the caller omits
// `--device`; real runs should pass the id from `list-devices`.
const DEFAULT_ANDROID_EMULATOR_SERIAL = "emulator-5554";

function createDriver(platform: string, deviceId?: string): Driver {
  switch (platform) {
    case "android":
      return new AndroidDriver({
        serial: deviceId ?? DEFAULT_ANDROID_EMULATOR_SERIAL,
      });
    case "ios":
      return new IosDriver({
        kind: "simulator",
        udid: deviceId ?? "",
      });
    default:
      throw new Error(
        `Unknown platform "${platform}". Use --platform android or --platform ios.`,
      );
  }
}

/**
 * Parse --proxy flag: "type:path" → CaptureConfig.
 * Examples:
 *   --proxy file:/tmp/capture.jsonl
 *   --proxy mitmproxy:/tmp/mitm.jsonl
 */
function parseCaptureFlag(
  flag: string | undefined,
): CaptureConfig | undefined {
  if (!flag) return undefined;
  const colonIdx = flag.indexOf(":");
  if (colonIdx <= 0) {
    return { type: "file", path: flag };
  }
  return {
    type: flag.slice(0, colonIdx),
    path: flag.slice(colonIdx + 1),
  };
}

function printTextResult(
  result: import("@atomyx/script").ScriptResult,
): void {
  const out = process.stdout.write.bind(process.stdout);
  out(`\n${result.ok ? "PASS" : "FAIL"} — ${result.scriptName}\n`);
  out(`  ${result.passedSteps}/${result.totalSteps} steps passed`);
  out(` (${result.durationMs}ms)\n\n`);

  for (const step of result.steps) {
    const icon = step.ok ? "✓" : "✗";
    const time = `${step.durationMs}ms`;
    out(
      `  ${icon} [${step.stepIndex + 1}] ${step.command} (${time})${step.detail ? ` — ${step.detail}` : ""}\n`,
    );
  }

  if (result.failedAtStep !== undefined) {
    out(`\n  Failed at step ${result.failedAtStep + 1}\n`);
  }

  const screenshots = result.artifacts.getScreenshots();
  if (screenshots.length > 0) {
    out(`\n  Screenshots: ${screenshots.map((s) => s.label).join(", ")}\n`);
  }
  out("\n");
}
