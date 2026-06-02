import {
  type JsonRpcErrorCode,
  type JsonRpcRequest,
  type JsonRpcResponse,
  errorResponse,
  successResponse,
} from "./types.js";

/**
 * A handler is the minimal unit the dispatcher knows about. It owns
 * its own params validation and may throw DispatcherError to control
 * the error code surfaced on the wire — everything else becomes
 * InternalError.
 *
 * Handlers are not coupled to transport or service internals — they
 * receive params and return results. Composition happens in the
 * entrypoint.
 */
export type Handler = (params: unknown) => Promise<unknown> | unknown;

export class DispatcherError extends Error {
  constructor(
    readonly code: JsonRpcErrorCode,
    message: string,
    readonly data?: unknown,
  ) {
    super(message);
    this.name = "DispatcherError";
  }
}

/**
 * Method → handler registry. One responsibility: route a request to
 * the right handler, wrap thrown errors into JsonRpcErrorResponse,
 * and serialize successful results.
 *
 * Intentionally knows nothing about IO streams, services, or the
 * session — that wiring is the entrypoint's job. Adding a new
 * method = `register()` call.
 */
export class Dispatcher {
  private readonly handlers = new Map<string, Handler>();

  register(method: string, handler: Handler): void {
    if (this.handlers.has(method)) {
      throw new Error(`Handler already registered for method "${method}"`);
    }
    this.handlers.set(method, handler);
  }

  async dispatch(request: JsonRpcRequest): Promise<JsonRpcResponse> {
    const handler = this.handlers.get(request.method);
    if (!handler) {
      return errorResponse(
        request.id,
        "MethodNotFound",
        `No handler registered for method "${request.method}"`,
      );
    }
    try {
      const result = await handler(request.params);
      return successResponse(request.id, result ?? null);
    } catch (err) {
      if (err instanceof DispatcherError) {
        return errorResponse(request.id, err.code, err.message, err.data);
      }
      const message = err instanceof Error ? err.message : String(err);
      return errorResponse(request.id, "InternalError", message);
    }
  }

  methods(): readonly string[] {
    return Array.from(this.handlers.keys()).sort();
  }
}
