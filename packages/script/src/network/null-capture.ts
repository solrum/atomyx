import type {
  CapturedRequest,
  NetworkCapture,
} from "@atomyx/shared/script";

/**
 * Null adapter — used when no MITM proxy is configured. Any
 * attempt to capture or wait for API requests throws a clear
 * error directing the user to configure a proxy.
 */
export class NullCapture implements NetworkCapture {
  async start(): Promise<void> {
    // no-op
  }

  async stop(): Promise<void> {
    // no-op
  }

  async waitForRequest(pattern: string): Promise<CapturedRequest> {
    throw new Error(
      `capture("${pattern}") requires a network capture adapter but none ` +
        `is configured. Pass --proxy <type>:<path> to the CLI, or set ` +
        `captureConfig in your script runner options.`,
    );
  }

  getAll(): readonly CapturedRequest[] {
    return [];
  }
}
