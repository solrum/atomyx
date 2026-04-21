import type {
  CaptureConfig,
  NetworkCapture,
} from "@atomyx/shared/script";
import { NullCapture } from "./null-capture.js";
import { FileCapture } from "./file-capture.js";

/**
 * Factory function type for creating NetworkCapture adapters.
 * Third-party adapters register via `registerCaptureAdapter`.
 */
export type CaptureAdapterFactory = (
  config: CaptureConfig,
) => NetworkCapture;

/**
 * Adapter registry — maps `config.type` to a factory function.
 * Built-in adapters are pre-registered. Third-party adapters
 * call `registerCaptureAdapter` to add themselves.
 *
 * Adding a new adapter:
 * 1. Implement `NetworkCapture` interface
 * 2. Call `registerCaptureAdapter("myType", (config) => new MyCapture(config))`
 * 3. Users pass `{ type: "myType", ... }` in their config
 */
const REGISTRY = new Map<string, CaptureAdapterFactory>();

// Built-in adapters
REGISTRY.set("none", () => new NullCapture());
REGISTRY.set("file", (config) => {
  if (!config.path) {
    throw new Error(
      `CaptureConfig type "file" requires a "path" to the JSON-lines capture file.`,
    );
  }
  return new FileCapture(config.path);
});
REGISTRY.set("mitmproxy", (config) => {
  // mitmproxy adapter uses the same file-based capture — the
  // mitmproxy addon writes JSON-lines to the specified path.
  // The distinction is semantic: "mitmproxy" signals that the
  // proxy addon should be running, "file" is passive read.
  if (!config.path) {
    throw new Error(
      `CaptureConfig type "mitmproxy" requires a "path" to the ` +
        `mitmproxy addon's JSON-lines output file.`,
    );
  }
  return new FileCapture(config.path);
});

/**
 * Register a third-party capture adapter. Call this before
 * creating a ScriptRunner if you need a custom proxy adapter.
 *
 * ```ts
 * import { registerCaptureAdapter } from "@atomyx/script";
 * registerCaptureAdapter("charles", (config) => new CharlesCapture(config));
 * ```
 */
export function registerCaptureAdapter(
  type: string,
  factory: CaptureAdapterFactory,
): void {
  REGISTRY.set(type, factory);
}

/**
 * Create a NetworkCapture adapter from config. Uses the
 * adapter registry to dispatch on `config.type`.
 *
 * Returns `NullCapture` when config is undefined or type is
 * "none".
 */
export function createCapture(
  config?: CaptureConfig,
): NetworkCapture {
  if (!config || config.type === "none") {
    return new NullCapture();
  }

  const factory = REGISTRY.get(config.type);
  if (!factory) {
    const available = [...REGISTRY.keys()].join(", ");
    throw new Error(
      `Unknown capture adapter type "${config.type}". ` +
        `Available: ${available}. ` +
        `Use registerCaptureAdapter() to add custom adapters.`,
    );
  }

  return factory(config);
}
