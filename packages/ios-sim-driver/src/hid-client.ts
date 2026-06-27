import net from "node:net";
import crypto from "node:crypto";

/** Normalised point with x/y in [0..1]. */
export interface NormalizedPoint {
  readonly x: number;
  readonly y: number;
}

export class HidClientError extends Error {
  constructor(
    message: string,
    public readonly code: "not-connected" | "timeout" | "reply-error" | "disconnected",
  ) {
    super(message);
    this.name = "HidClientError";
  }
}

export interface HidClientOptions {
  readonly port: number;
  readonly signal?: AbortSignal;
  /** Per-dispatch reply timeout (ms). Defaults to 2000. */
  readonly dispatchTimeoutMs?: number;
}

interface PendingReply {
  resolve: () => void;
  reject: (err: Error) => void;
  timer?: NodeJS.Timeout;
}

/**
 * WebSocket client for the atomyx-sim-hid helper process.
 *
 * Protocol (helper-defined):
 *   → newline-delimited JSON gesture messages
 *   ← newline-delimited JSON replies: {"ok":true|false,"id":N,"error":"..."}
 *
 * Implements the RFC 6455 opening handshake manually over node:net
 * because Node 20 does not ship a global WebSocket client.
 */
export class HidClient {
  private socket: net.Socket | null = null;
  // Accumulates raw binary data arriving from the server after the WS upgrade.
  private rawBuffer = Buffer.alloc(0);
  private nextId = 1;
  private readonly pending = new Map<number, PendingReply>();
  private dead = true;
  private readonly opts: HidClientOptions;

  constructor(opts: HidClientOptions) {
    this.opts = opts;
  }

  async connect(): Promise<void> {
    const { port, signal } = this.opts;
    await this.openWs(port, signal);
  }

  async tap(point: NormalizedPoint, holdMs = 50): Promise<void> {
    await this.dispatch({ type: "tap", x: point.x, y: point.y, holdMs });
  }

  async swipe(
    from: NormalizedPoint,
    to: NormalizedPoint,
    durationMs: number,
  ): Promise<void> {
    // Map durationMs to (steps, stepMs) matching helper expectations.
    // steps × stepMs ≈ durationMs; cap steps at 60 to stay under budget.
    const stepMs = 16;
    const steps = Math.max(1, Math.min(60, Math.round(durationMs / stepMs)));
    await this.dispatch({
      type: "swipe",
      x1: from.x,
      y1: from.y,
      x2: to.x,
      y2: to.y,
      steps,
      stepMs,
      dwellMs: 0,
    });
  }

  async touchDown(point: NormalizedPoint, id: number): Promise<void> {
    await this.dispatch({ type: "touch-down", x: point.x, y: point.y, id });
  }

  async touchMove(point: NormalizedPoint, id: number): Promise<void> {
    await this.dispatch({ type: "touch-move", x: point.x, y: point.y, id });
  }

  async touchUp(point: NormalizedPoint, id: number): Promise<void> {
    await this.dispatch({ type: "touch-up", x: point.x, y: point.y, id });
  }

  async dispose(): Promise<void> {
    this.dead = true;
    if (this.socket) {
      this.socket.destroy();
      this.socket = null;
    }
    const err = new HidClientError("client disposed", "disconnected");
    for (const [, p] of this.pending) {
      if (p.timer) clearTimeout(p.timer);
      p.reject(err);
    }
    this.pending.clear();
  }

  private async openWs(port: number, signal?: AbortSignal): Promise<void> {
    return new Promise<void>((resolve, reject) => {
      if (signal?.aborted) {
        reject(new HidClientError("connect aborted", "not-connected"));
        return;
      }

      const sock = net.createConnection({ host: "127.0.0.1", port });

      const onAbort = () => {
        sock.destroy();
        reject(new HidClientError("connect aborted", "not-connected"));
      };
      signal?.addEventListener("abort", onAbort, { once: true });

      sock.once("error", (err) => {
        signal?.removeEventListener("abort", onAbort);
        reject(new HidClientError(`WS connect failed: ${err.message}`, "not-connected"));
      });

      sock.once("connect", () => {
        // RFC 6455 opening handshake.
        const key = crypto.randomBytes(16).toString("base64");
        const handshake = [
          `GET / HTTP/1.1`,
          `Host: 127.0.0.1:${port}`,
          `Upgrade: websocket`,
          `Connection: Upgrade`,
          `Sec-WebSocket-Key: ${key}`,
          `Sec-WebSocket-Version: 13`,
          `\r\n`,
        ].join("\r\n");
        sock.write(handshake);

        // Wait for the HTTP 101 Switching Protocols response.
        let header = "";
        const onHeaderData = (chunk: Buffer) => {
          header += chunk.toString("binary");
          const end = header.indexOf("\r\n\r\n");
          if (end < 0) return;

          // Validate upgrade response.
          if (!header.startsWith("HTTP/1.1 101")) {
            sock.destroy();
            reject(
              new HidClientError(
                `WS upgrade rejected: ${header.split("\r\n")[0]}`,
                "not-connected",
              ),
            );
            return;
          }

          // Extract any data that arrived after the headers (start of WS frames).
          const leftover = Buffer.from(header.slice(end + 4), "binary");

          sock.removeListener("data", onHeaderData);
          signal?.removeEventListener("abort", onAbort);

          // Switch to WS frame mode — keep encoding as binary.
          this.socket = sock;
          this.dead = false;

          sock.on("data", (chunk: Buffer) => this.onRawData(chunk));
          sock.on("error", (err) =>
            this.handleDisconnect(`socket error: ${err.message}`),
          );
          sock.on("close", () => this.handleDisconnect("socket closed"));

          if (leftover.length > 0) {
            this.onRawData(leftover);
          }

          resolve();
        };
        sock.on("data", onHeaderData);
      });
    });
  }

  /**
   * Accumulates raw binary data from the server and extracts complete
   * WS frames (RFC 6455 §5.2). The helper never sends masked frames
   * (server-to-client, RFC 6455 §5.1) and always sends text frames
   * (opcode 0x1) containing newline-terminated JSON.
   */
  private onRawData(chunk: Buffer): void {
    this.rawBuffer = Buffer.concat([this.rawBuffer, chunk]);
    while (true) {
      const frame = this.extractWsFrame();
      if (!frame) break;
      if (frame.length > 0) this.handleLine(frame.toString("utf8").trim());
    }
  }

  /**
   * Attempts to parse and consume one complete WS text frame from
   * `rawBuffer`. Returns the frame payload Buffer on success, or
   * null when there is not yet enough data.
   */
  private extractWsFrame(): Buffer | null {
    const buf = this.rawBuffer;
    if (buf.length < 2) return null;
    const opcode = buf[0]! & 0x0f;
    // We only care about text frames (0x1) and ignore control frames.
    const isMasked = !!(buf[1]! & 0x80);
    let lenByte = buf[1]! & 0x7f;
    let offset = 2;
    if (lenByte === 126) {
      if (buf.length < 4) return null;
      lenByte = buf.readUInt16BE(2);
      offset = 4;
    } else if (lenByte === 127) {
      // 8-byte extended length — helper never sends payloads this large.
      if (buf.length < 10) return null;
      lenByte = Number(buf.readBigUInt64BE(2));
      offset = 10;
    }
    const maskLen = isMasked ? 4 : 0;
    const totalLen = offset + maskLen + lenByte;
    if (buf.length < totalLen) return null;
    const maskBytes = isMasked ? buf.slice(offset, offset + 4) : null;
    offset += maskLen;
    const payload = Buffer.from(buf.slice(offset, offset + lenByte));
    if (maskBytes) {
      for (let i = 0; i < payload.length; i++) {
        payload[i] ^= maskBytes[i % 4]!;
      }
    }
    this.rawBuffer = buf.slice(totalLen);
    if (opcode !== 0x1) return Buffer.alloc(0); // skip non-text frames, consume bytes
    return payload;
  }

  private handleLine(line: string): void {
    let msg: { ok?: boolean; id?: number; error?: string };
    try {
      msg = JSON.parse(line);
    } catch {
      return;
    }
    if (typeof msg.id !== "number") return;
    const pending = this.pending.get(msg.id);
    if (!pending) return;
    this.pending.delete(msg.id);
    if (pending.timer) clearTimeout(pending.timer);
    if (msg.ok === true) {
      pending.resolve();
    } else {
      pending.reject(
        new HidClientError(
          msg.error ?? `hid-client: dispatch id=${msg.id} returned ok:false`,
          "reply-error",
        ),
      );
    }
  }

  private handleDisconnect(reason: string): void {
    if (this.dead) return;
    this.dead = true;
    this.socket = null;
    const err = new HidClientError(`hid-client disconnected: ${reason}`, "disconnected");
    for (const [, p] of this.pending) {
      if (p.timer) clearTimeout(p.timer);
      p.reject(err);
    }
    this.pending.clear();
  }

  private dispatch(payload: Record<string, unknown>): Promise<void> {
    if (!this.socket || this.dead) {
      return Promise.reject(
        new HidClientError(
          "hid-client: not connected — call connect() first",
          "not-connected",
        ),
      );
    }
    const id = this.nextId++;
    const timeoutMs = this.opts.dispatchTimeoutMs ?? 2_000;
    const line = JSON.stringify({ ...payload, id }) + "\n";

    // The helper accepts raw text frames. Send as a WS text frame:
    // FIN=1, opcode=0x1 (text), mask=1, payload.
    const payloadBuf = Buffer.from(line, "utf8");
    const payloadLen = payloadBuf.length;
    const mask = crypto.randomBytes(4);

    let header: Buffer;
    if (payloadLen <= 125) {
      header = Buffer.alloc(6);
      header[0] = 0x81; // FIN + text opcode
      header[1] = 0x80 | payloadLen; // mask bit + length
      mask.copy(header, 2);
    } else if (payloadLen <= 65535) {
      header = Buffer.alloc(8);
      header[0] = 0x81;
      header[1] = 0x80 | 126;
      header.writeUInt16BE(payloadLen, 2);
      mask.copy(header, 4);
    } else {
      // Gestures never exceed 65535 bytes; guard anyway.
      return Promise.reject(
        new HidClientError("hid-client: payload too large", "not-connected"),
      );
    }

    const masked = Buffer.alloc(payloadLen);
    for (let i = 0; i < payloadLen; i++) {
      masked[i] = payloadBuf[i]! ^ mask[i % 4]!;
    }

    return new Promise<void>((resolve, reject) => {
      const entry: PendingReply = { resolve, reject };
      entry.timer = setTimeout(() => {
        if (this.pending.has(id)) {
          this.pending.delete(id);
          reject(
            new HidClientError(
              `hid-client: dispatch id=${id} timed out after ${timeoutMs}ms`,
              "timeout",
            ),
          );
        }
      }, timeoutMs);
      this.pending.set(id, entry);
      try {
        this.socket!.write(Buffer.concat([header, masked]));
      } catch (err) {
        this.pending.delete(id);
        if (entry.timer) clearTimeout(entry.timer);
        reject(err as Error);
      }
    });
  }
}
