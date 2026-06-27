/**
 * Tiny HTTP client used by the Android driver to talk to the
 * APK's control server over a forwarded loopback port. Uses
 * native `fetch` (Node 20+), with a per-request timeout and a
 * structured error type so transport failures can be surfaced as
 * Driver-level errors rather than opaque fetch rejections.
 *
 * Framework note: this file does NOT import Zod schemas — wire
 * validation is the AndroidDriver's responsibility, happening
 * one layer up. Keeping the HTTP client schema-agnostic makes it
 * trivial to reuse for future platforms that speak HTTP+JSON
 * (e.g. a Web driver) without coupling to any specific route
 * shape.
 */

export class HttpClientError extends Error {
  constructor(
    message: string,
    public readonly url: string,
    public readonly status?: number,
    public readonly body?: string,
  ) {
    super(message);
    this.name = "HttpClientError";
  }
}

export interface HttpClientOptions {
  readonly baseUrl: string;
  readonly defaultTimeoutMs?: number;
}

export class HttpClient {
  constructor(private readonly opts: HttpClientOptions) {}

  private url(path: string): string {
    return `${this.opts.baseUrl}${path.startsWith("/") ? path : "/" + path}`;
  }

  async get<T = unknown>(
    path: string,
    opts?: { timeoutMs?: number; signal?: AbortSignal },
  ): Promise<T> {
    return this.request<T>("GET", path, undefined, opts);
  }

  async post<T = unknown>(
    path: string,
    body: unknown,
    opts?: { timeoutMs?: number; signal?: AbortSignal },
  ): Promise<T> {
    return this.request<T>("POST", path, body, opts);
  }

  private async request<T>(
    method: "GET" | "POST",
    path: string,
    body: unknown,
    opts?: { timeoutMs?: number; signal?: AbortSignal },
  ): Promise<T> {
    const url = this.url(path);
    const timeoutMs = opts?.timeoutMs ?? this.opts.defaultTimeoutMs ?? 10_000;
    // Merge the optional external signal with our internal timeout
    // controller. Either path (external abort OR per-request
    // timeout) aborts the in-flight fetch. The external signal's
    // reason is preserved so AbortError propagation upstream stays
    // honest about WHY the request was cancelled.
    const controller = new AbortController();
    const onExternalAbort = () => controller.abort(opts?.signal?.reason);
    if (opts?.signal) {
      if (opts.signal.aborted) {
        controller.abort(opts.signal.reason);
      } else {
        opts.signal.addEventListener("abort", onExternalAbort, { once: true });
      }
    }
    const timer = setTimeout(() => controller.abort(), timeoutMs);
    try {
      const res = await fetch(url, {
        method,
        headers:
          method === "POST"
            ? { "content-type": "application/json" }
            : undefined,
        body: body !== undefined ? JSON.stringify(body) : undefined,
        signal: controller.signal,
      });
      if (!res.ok) {
        const text = await res.text().catch(() => "");
        throw new HttpClientError(
          `${method} ${path} → ${res.status} ${res.statusText}`,
          url,
          res.status,
          text,
        );
      }
      // 204 No Content — return undefined typed as T
      if (res.status === 204) return undefined as T;
      const contentType = res.headers.get("content-type") ?? "";
      if (contentType.includes("application/json")) {
        return (await res.json()) as T;
      }
      // Fall through: return raw text for non-JSON responses.
      return (await res.text()) as T;
    } catch (err) {
      if (err instanceof HttpClientError) throw err;
      const e = err as Error;
      if (e.name === "AbortError") {
        // Re-throw the external signal's AbortError verbatim so
        // callers can detect intentional cancellation distinctly
        // from a transport-side timeout.
        if (opts?.signal?.aborted) {
          throw opts.signal.reason ?? e;
        }
        throw new HttpClientError(
          `${method} ${path} timed out after ${timeoutMs}ms`,
          url,
        );
      }
      throw new HttpClientError(
        `${method} ${path} failed: ${e.message}`,
        url,
      );
    } finally {
      clearTimeout(timer);
      if (opts?.signal) {
        opts.signal.removeEventListener("abort", onExternalAbort);
      }
    }
  }
}
