import { readFileSync, watchFile, unwatchFile, existsSync } from "node:fs";
import type {
  CapturedRequest,
  NetworkCapture,
} from "@atomyx/shared/script";

/**
 * File-based capture adapter — reads HTTP traffic from a
 * JSON-lines file. Each line is a JSON object matching the
 * `CapturedRequest` shape.
 *
 * This is the generic adapter that works with any proxy tool
 * that can output JSON-lines (mitmproxy, custom scripts, etc).
 * The Atomyx mitmproxy addon writes to this format natively.
 *
 * Line format:
 * ```json
 * {"method":"POST","url":"https://api.example.com/transfer","status":200,"headers":{},"body":{"ok":true},"timestamp":1713300000000}
 * ```
 */
/**
 * Poll cadence for both the fs watcher and the in-memory match
 * loop inside `waitForRequest`. Short enough to keep script
 * perception latency low; long enough to avoid hammering the
 * capture file's inode. Same value on both sides so the two
 * loops stay aligned — a request appearing in the file within
 * one interval is observed by both on the next tick.
 */
const CAPTURE_POLL_INTERVAL_MS = 100;

export class FileCapture implements NetworkCapture {
  private requests: CapturedRequest[] = [];
  private watching = false;
  private lastSize = 0;

  constructor(private readonly filePath: string) {}

  async start(): Promise<void> {
    this.loadExisting();
    this.watching = true;
    watchFile(this.filePath, { interval: CAPTURE_POLL_INTERVAL_MS }, () => {
      this.loadNew();
    });
  }

  async stop(): Promise<void> {
    if (this.watching) {
      unwatchFile(this.filePath);
      this.watching = false;
    }
  }

  async waitForRequest(
    pattern: string,
    timeoutMs = 10_000,
  ): Promise<CapturedRequest> {
    const { method, path } = parsePattern(pattern);
    const deadline = Date.now() + timeoutMs;

    while (Date.now() < deadline) {
      this.loadNew();
      const match = this.findMatch(method, path);
      if (match) return match;
      await sleep(CAPTURE_POLL_INTERVAL_MS);
    }

    throw new Error(
      `Timed out waiting for "${pattern}" after ${timeoutMs}ms. ` +
        `Captured ${this.requests.length} requests total. ` +
        `Check that the proxy is running and the app routes through it.`,
    );
  }

  getAll(): readonly CapturedRequest[] {
    this.loadNew();
    return [...this.requests];
  }

  private loadExisting(): void {
    if (!existsSync(this.filePath)) {
      this.requests = [];
      this.lastSize = 0;
      return;
    }
    const content = readFileSync(this.filePath, "utf-8");
    this.lastSize = content.length;
    this.requests = parseLines(content);
  }

  private loadNew(): void {
    if (!existsSync(this.filePath)) return;
    const content = readFileSync(this.filePath, "utf-8");
    if (content.length <= this.lastSize) return;
    const newPart = content.slice(this.lastSize);
    this.lastSize = content.length;
    this.requests.push(...parseLines(newPart));
  }

  private findMatch(
    method: string | null,
    path: string,
  ): CapturedRequest | undefined {
    for (let i = this.requests.length - 1; i >= 0; i--) {
      const req = this.requests[i]!;
      if (method && req.method.toUpperCase() !== method) continue;
      if (matchesPath(req.url, path)) return req;
    }
    return undefined;
  }
}

/**
 * Parse a capture pattern like "POST /api/transfer" into
 * { method, path }. If no method prefix, matches any method.
 */
function parsePattern(pattern: string): {
  method: string | null;
  path: string;
} {
  const match = pattern.match(
    /^(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)\s+(.+)$/i,
  );
  if (match) {
    return { method: match[1]!.toUpperCase(), path: match[2]!.trim() };
  }
  return { method: null, path: pattern.trim() };
}

function matchesPath(url: string, pattern: string): boolean {
  try {
    const urlPath = new URL(url).pathname;
    if (pattern.startsWith("/")) {
      if (pattern.endsWith("*")) {
        return urlPath.startsWith(pattern.slice(0, -1));
      }
      return urlPath === pattern;
    }
    return url.includes(pattern);
  } catch {
    return url.includes(pattern);
  }
}

function parseLines(content: string): CapturedRequest[] {
  const results: CapturedRequest[] = [];
  for (const line of content.split("\n")) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    try {
      const parsed = JSON.parse(trimmed) as CapturedRequest;
      if (parsed.method && parsed.url && typeof parsed.status === "number") {
        results.push(parsed);
      }
    } catch {
      // Skip malformed lines
    }
  }
  return results;
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
