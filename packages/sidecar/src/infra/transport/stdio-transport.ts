import type { Readable, Writable } from "node:stream";
import { createInterface } from "node:readline";
import type { Dispatcher } from "./dispatcher.js";
import {
  type JsonRpcEvent,
  type JsonRpcResponse,
  errorResponse,
  isRequest,
} from "./types.js";

export interface StdioTransportDeps {
  readonly input: Readable;
  readonly output: Writable;
  readonly dispatcher: Dispatcher;
  readonly onError?: (err: unknown) => void;
}

/**
 * Line-delimited JSON-RPC transport over a readable / writable pair.
 *
 * Knows nothing about the host — the caller passes in the streams
 * (process.stdin / process.stdout in production, PassThrough pair in
 * tests). Reads one JSON document per line, hands it to the
 * dispatcher, writes the response back on its own line.
 *
 * Events are pushed via `emit()` — services do not write to stdout
 * themselves, they go through the transport so a future change of
 * encoding or channel stays contained in this class.
 */
export class StdioTransport {
  private readonly input: Readable;
  private readonly output: Writable;
  private readonly dispatcher: Dispatcher;
  private readonly onError: (err: unknown) => void;
  private started = false;

  constructor(deps: StdioTransportDeps) {
    this.input = deps.input;
    this.output = deps.output;
    this.dispatcher = deps.dispatcher;
    this.onError = deps.onError ?? (() => {});
  }

  start(): void {
    if (this.started) return;
    this.started = true;
    const rl = createInterface({ input: this.input, crlfDelay: Infinity });
    rl.on("line", (line) => {
      void this.handleLine(line);
    });
    rl.on("close", () => {
      /* stream closed — nothing to do; caller decides when to exit */
    });
    rl.on("error", (err) => this.onError(err));
  }

  emit(event: JsonRpcEvent): void {
    this.writeLine(event);
  }

  private async handleLine(line: string): Promise<void> {
    const trimmed = line.trim();
    if (trimmed.length === 0) return;
    let parsed: unknown;
    try {
      parsed = JSON.parse(trimmed);
    } catch {
      this.writeLine(
        errorResponse("parse-error", "ParseError", "Invalid JSON received"),
      );
      return;
    }
    if (!isRequest(parsed)) {
      this.writeLine(
        errorResponse(
          "invalid-request",
          "InvalidRequest",
          "Message missing required fields (id, method)",
        ),
      );
      return;
    }
    try {
      const response = await this.dispatcher.dispatch(parsed);
      this.writeLine(response);
    } catch (err) {
      this.onError(err);
      this.writeLine(
        errorResponse(
          parsed.id,
          "InternalError",
          err instanceof Error ? err.message : String(err),
        ),
      );
    }
  }

  private writeLine(payload: JsonRpcResponse | JsonRpcEvent): void {
    try {
      this.output.write(`${JSON.stringify(payload)}\n`);
    } catch (err) {
      this.onError(err);
    }
  }
}
