/**
 * NetworkCapture port — abstract interface for capturing HTTP
 * traffic during a test script run. Read-only by design: the
 * script engine observes API responses but never modifies them.
 *
 * Concrete implementations:
 *   - `MitmproxyCapture` — reads from mitmproxy JSON-lines output
 *   - `NullCapture` — throws when capture is attempted without proxy
 *   - Future: Charles, Proxyman, custom adapters
 *
 * Adding a new adapter = implementing this interface. No runner
 * or command changes needed.
 */

export interface CapturedRequest {
  /** HTTP method (GET, POST, PUT, etc.). */
  readonly method: string;
  /** Full request URL. */
  readonly url: string;
  /** HTTP response status code. */
  readonly status: number;
  /** Response headers. */
  readonly headers: Readonly<Record<string, string>>;
  /** Parsed response body (JSON or null for non-JSON). */
  readonly body: unknown;
  /** Unix timestamp (ms) when the response was captured. */
  readonly timestamp: number;
}

/**
 * Configuration for creating a NetworkCapture adapter. Lives in
 * `shared` so any consumer can serialize and deserialize a
 * capture config without depending on the implementation packages
 * that provide concrete adapters.
 *
 * The `type` field is an extensible string — concrete adapters
 * register themselves against it via the capture-adapter factory.
 * Adding a new adapter type is: add a string literal here + the
 * adapter class + a factory entry. No existing code changes.
 */
export interface CaptureConfig {
  /**
   * Adapter type. Built-in values:
   *   - `"none"` — no proxy, capture commands throw
   *   - `"file"` — read from a JSON-lines capture file
   *   - `"mitmproxy"` — mitmproxy with Atomyx addon
   *
   * Third-party adapters can use any string (e.g. `"charles"`,
   * `"proxyman"`, `"har"`).
   */
  readonly type: string;
  /** Path to the capture file (for file-based adapters). */
  readonly path?: string;
  /** Proxy listen port (for adapters that manage a proxy). */
  readonly port?: number;
  /** Proxy listen host. Default: 127.0.0.1. */
  readonly host?: string;
}

export interface NetworkCapture {
  /** Begin capturing traffic. */
  start(): Promise<void>;
  /** Stop capturing and release resources. */
  stop(): Promise<void>;
  /**
   * Wait for a request matching `pattern` to appear.
   * Pattern format: "METHOD /path" (e.g. "POST /api/transfer").
   * Throws on timeout.
   */
  waitForRequest(
    pattern: string,
    timeoutMs?: number,
  ): Promise<CapturedRequest>;
  /** Return all captured requests so far. */
  getAll(): readonly CapturedRequest[];
}
