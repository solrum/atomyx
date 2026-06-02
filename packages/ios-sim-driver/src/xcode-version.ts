import { execSync } from "node:child_process";
import { join } from "node:path";
import os from "node:os";

/**
 * Xcode capability probe for the Sim-direct HID adapter.
 *
 * Returns `true` when ALL of the following hold:
 *
 *   - The host CPU is arm64 (Apple Silicon). The
 *     SimDeviceLegacyHIDClient private framework is present only on
 *     arm64 macOS builds of Xcode.
 *   - Xcode major version meets the verified floor.
 *     SimDeviceLegacyHIDClient + IndigoHIDClient required symbols
 *     (createDigitizer, createFinger, append, trackpadWrap) are
 *     present in Xcode 16.x; the helper itself runtime-probes each
 *     symbol and emits a `listen` handshake only when all resolve.
 *
 * When any check throws (Xcode not installed, `xcode-select` absent,
 * malformed plist) the function returns `false` — the safe fallback
 * preserves the XCUITest path.
 *
 * Returning `true` here means the system COULD run the HID path; the
 * caller (device factory) still requires `ATOMYX_SIM_HID=1` for
 * explicit opt-in.
 */

let cachedResult: boolean | undefined;

/**
 * Minimum verified Xcode major version. Xcode 16.x has been probed
 * to expose the SimDeviceLegacyHIDClient and IndigoHIDClient symbols
 * the helper needs; the helper additionally probes each symbol at
 * load time and degrades gracefully if any are missing.
 */
const REQUIRED_XCODE_MAJOR = 16;

/** Exposed for tests only — resets the memo so mocks take effect. */
export function _resetSimDirectCache(): void {
  cachedResult = undefined;
}

export function isSimDirectSupported(): boolean {
  if (cachedResult !== undefined) return cachedResult;
  cachedResult = checkSimDirectSupported();
  return cachedResult;
}

function checkSimDirectSupported(): boolean {
  try {
    if (os.arch() !== "arm64") {
      process.stderr.write(`[sim-direct] arch=${os.arch()} (not arm64)\n`);
      return false;
    }

    const developerDir = execSync("xcode-select -p", { encoding: "utf8" })
      .trim();
    if (!developerDir) {
      process.stderr.write(`[sim-direct] xcode-select returned empty\n`);
      return false;
    }

    // Xcode's Info.plist is binary on shipping builds; XML regex
    // parsing does not work. plutil ships with macOS and reads both
    // formats; the `raw` output is just the value.
    const plistPath = join(developerDir, "../Info.plist");
    const dtxcodeStr = execSync(
      `plutil -extract DTXcode raw ${JSON.stringify(plistPath)}`,
      { encoding: "utf8" },
    ).trim();
    if (!dtxcodeStr) {
      process.stderr.write(`[sim-direct] plutil returned empty for ${plistPath}\n`);
      return false;
    }

    // DTXcode is a zero-padded decimal: "1620" means 16.2, "2600"
    // means 26.0. Divide by 100 to get the major version.
    const dtxcode = parseInt(dtxcodeStr, 10);
    if (!Number.isFinite(dtxcode)) {
      process.stderr.write(`[sim-direct] dtxcode parse failed for "${dtxcodeStr}"\n`);
      return false;
    }
    const majorVersion = Math.floor(dtxcode / 100);
    const ok = majorVersion >= REQUIRED_XCODE_MAJOR;
    process.stderr.write(
      `[sim-direct] dev=${developerDir} dtxcode=${dtxcode} major=${majorVersion} floor=${REQUIRED_XCODE_MAJOR} ok=${ok}\n`,
    );
    return ok;
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    process.stderr.write(`[sim-direct] check threw: ${msg}\n`);
    return false;
  }
}
