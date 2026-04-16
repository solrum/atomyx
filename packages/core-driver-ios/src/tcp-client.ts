import net from "node:net";

/**
 * Line-delimited JSON TCP client for the Swift XCUITest driver.
 *
 * Wire protocol (frozen by the Swift driver's CommandRegistry):
 *
 *   →  { id, type, args }     (newline-terminated JSON line)
 *   ←  { id, ok: true,  data }
 *   ←  { id, ok: false, error }
 *
 * Every request carries a monotonically-increasing `id` the client
 * correlates with the response. The Swift side always echoes the
 * id back, so out-of-order responses are legal (though the current
 * driver dispatches commands sequentially).
 *
 * Unlike the legacy `IosXctestController` adapter this file is
 * replacing, there is NO selector resolution, scroll logic,
 * obscurement detection, or lifecycle juggling here. This is a
 * pure transport — the IosDriver class above it calls `.call(type,
 * args)` and waits for the structured response, nothing more.
 *
 * Errors:
 *
 *   - `TcpClientError` with `code === "not-connected"` when a call
 *     fires before `connect()` has resolved (or after
 *     `disconnect()`).
 *   - `TcpClientError` with `code === "driver-error"` when the
 *     Swift side returns `{ok:false}`. The error carries the
 *     original string from the driver verbatim.
 *   - `TcpClientError` with `code === "disconnected"` when the
 *     socket closes mid-request. All pending waiters reject with
 *     this and `connectionDead` flips true; callers must call
 *     `reconnect()` to recover.
 */

export class TcpClientError extends Error {
  constructor(
    message: string,
    public readonly code: "not-connected" | "driver-error" | "disconnected" | "parse-error" | "timeout",
    public readonly requestType?: string,
  ) {
    super(message);
    this.name = "TcpClientError";
  }
}

export interface TcpClientOptions {
  readonly host: string;
  readonly port: number;
  /** Connect retry budget when the driver takes time to come up. */
  readonly connectTimeoutMs?: number;
  /** Per-request timeout for matching a response to a pending id. */
  readonly requestTimeoutMs?: number;
}

interface Pending {
  readonly type: string;
  resolve: (data: Record<string, unknown>) => void;
  reject: (err: Error) => void;
  timer?: NodeJS.Timeout;
}

export class TcpClient {
  private socket: net.Socket | null = null;
  private buffer = "";
  private nextId = 1;
  private pending = new Map<number, Pending>();
  private connectionDead = true;

  constructor(private readonly opts: TcpClientOptions) {}

  isConnected(): boolean {
    return this.socket !== null && !this.connectionDead;
  }

  /**
   * Open the TCP connection with retry. The Swift driver can take
   * several seconds to start listening after `xcodebuild test`
   * spawns the XCUITest runner; polling the socket up to
   * `connectTimeoutMs` absorbs that cold start window without
   * forcing the caller to script their own retry.
   */
  async connect(): Promise<void> {
    const deadline = Date.now() + (this.opts.connectTimeoutMs ?? 30_000);
    let lastError: Error | null = null;
    while (Date.now() < deadline) {
      try {
        await this.openSocket();
        return;
      } catch (err) {
        lastError = err as Error;
        await new Promise((r) => setTimeout(r, 500));
      }
    }
    throw new TcpClientError(
      `iOS driver not reachable on ${this.opts.host}:${this.opts.port} — ${lastError?.message ?? "unknown"}`,
      "not-connected",
    );
  }

  /**
   * Re-open the socket after a disconnect. Clears stale pending
   * waiters (they've already been rejected by `handleDisconnect`)
   * and resets the buffer. Does NOT reset `nextId` — the Swift
   * driver tolerates sparse id sequences.
   */
  async reconnect(): Promise<void> {
    if (this.socket) {
      this.socket.destroy();
      this.socket = null;
    }
    this.buffer = "";
    this.connectionDead = false;
    await this.connect();
  }

  private openSocket(): Promise<void> {
    return new Promise((resolve, reject) => {
      const sock = net.createConnection({
        host: this.opts.host,
        port: this.opts.port,
      });
      const onError = (err: Error) => {
        sock.removeAllListeners();
        sock.destroy();
        reject(err);
      };
      sock.once("error", onError);
      sock.once("connect", () => {
        sock.removeListener("error", onError);
        sock.setEncoding("utf8");
        sock.on("data", (chunk: string) => this.onData(chunk));
        sock.on("error", (err) => this.handleDisconnect(`socket error: ${err.message}`));
        sock.on("close", () => this.handleDisconnect("socket closed"));
        this.socket = sock;
        this.connectionDead = false;
        resolve();
      });
    });
  }

  private onData(chunk: string): void {
    this.buffer += chunk;
    let newlineIdx: number;
    while ((newlineIdx = this.buffer.indexOf("\n")) >= 0) {
      const line = this.buffer.slice(0, newlineIdx);
      this.buffer = this.buffer.slice(newlineIdx + 1);
      if (line.length === 0) continue;
      this.handleLine(line);
    }
  }

  private handleLine(line: string): void {
    let msg: {
      id?: number;
      ok?: boolean;
      data?: Record<string, unknown>;
      error?: string;
    };
    try {
      msg = JSON.parse(line);
    } catch {
      // Ignore parse errors — the Swift side sometimes emits log
      // lines on stdout before the test runner starts serving.
      return;
    }
    if (typeof msg.id !== "number") return;
    const waiter = this.pending.get(msg.id);
    if (!waiter) return;
    this.pending.delete(msg.id);
    if (waiter.timer) clearTimeout(waiter.timer);
    if (msg.ok === true) {
      waiter.resolve(msg.data ?? {});
    } else {
      waiter.reject(
        new TcpClientError(
          msg.error ?? `driver returned ok:false for ${waiter.type}`,
          "driver-error",
          waiter.type,
        ),
      );
    }
  }

  /**
   * Shared cleanup path for socket close + error. All pending
   * waiters reject with a disconnect error and the socket is
   * marked dead. Caller must invoke `reconnect()` before the
   * next `call()`.
   */
  private handleDisconnect(reason: string): void {
    if (this.connectionDead) return; // idempotent — close + error can both fire
    this.connectionDead = true;
    this.socket = null;
    const err = new TcpClientError(`driver disconnected: ${reason}`, "disconnected");
    for (const [, waiter] of this.pending) {
      if (waiter.timer) clearTimeout(waiter.timer);
      waiter.reject(err);
    }
    this.pending.clear();
  }

  /**
   * Issue a command. Resolves with the `data` object from the
   * matching `{ok:true}` response, or rejects with a
   * `TcpClientError`. Per-request timeout is optional — omit to
   * wait indefinitely (sensible for `launchApp` which can take
   * 10+ seconds on cold starts).
   */
  async call(
    type: string,
    args: Record<string, unknown> = {},
    timeoutMs?: number,
  ): Promise<Record<string, unknown>> {
    if (!this.socket || this.connectionDead) {
      throw new TcpClientError(
        `call(${type}) before connect(), or after disconnect — call reconnect() first`,
        "not-connected",
        type,
      );
    }
    const id = this.nextId++;
    const line = JSON.stringify({ id, type, args }) + "\n";
    return new Promise<Record<string, unknown>>((resolve, reject) => {
      const waiter: Pending = { type, resolve, reject };
      if (timeoutMs !== undefined) {
        waiter.timer = setTimeout(() => {
          if (this.pending.has(id)) {
            this.pending.delete(id);
            reject(
              new TcpClientError(
                `call(${type}) timed out after ${timeoutMs}ms`,
                "timeout",
                type,
              ),
            );
          }
        }, timeoutMs);
      }
      this.pending.set(id, waiter);
      try {
        this.socket!.write(line);
      } catch (err) {
        this.pending.delete(id);
        if (waiter.timer) clearTimeout(waiter.timer);
        reject(err as Error);
      }
    });
  }

  async disconnect(): Promise<void> {
    if (this.socket) {
      this.socket.destroy();
      this.socket = null;
    }
    this.connectionDead = true;
    this.buffer = "";
    for (const [, waiter] of this.pending) {
      if (waiter.timer) clearTimeout(waiter.timer);
      waiter.reject(new TcpClientError("client disconnected by caller", "disconnected"));
    }
    this.pending.clear();
  }
}
