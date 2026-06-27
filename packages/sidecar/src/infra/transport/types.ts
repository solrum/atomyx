/**
 * Wire protocol shapes for the sidecar bridge.
 *
 * A message on stdin/stdout is one line of JSON. Three variants:
 *   - JsonRpcRequest: caller → sidecar, must carry an id.
 *   - JsonRpcResponse: sidecar → caller, echoes the request id.
 *   - JsonRpcEvent: sidecar → caller, no id — streamed out-of-band
 *     for progress, device-state changes, script step updates.
 *
 * Kept framework-free so any transport (stdio, pipe, socket) can
 * read / write the same JSON.
 */

export interface JsonRpcRequest {
  readonly id: string;
  readonly method: string;
  readonly params?: unknown;
}

export interface JsonRpcSuccessResponse {
  readonly id: string;
  readonly result: unknown;
}

export interface JsonRpcErrorResponse {
  readonly id: string;
  readonly error: JsonRpcError;
}

export type JsonRpcResponse = JsonRpcSuccessResponse | JsonRpcErrorResponse;

export interface JsonRpcError {
  readonly code: JsonRpcErrorCode;
  readonly message: string;
  readonly data?: unknown;
}

export type JsonRpcErrorCode =
  | "ParseError"
  | "InvalidRequest"
  | "MethodNotFound"
  | "InvalidParams"
  | "InternalError"
  | "NotConnected"
  | "NoDeviceSelected"
  | "DeviceBusy"
  | "InvalidScript"
  | "SelectorNotFound"
  | "Timeout"
  | "PlatformError";

export interface JsonRpcEvent {
  readonly event: string;
  readonly payload: unknown;
}

export function isRequest(msg: unknown): msg is JsonRpcRequest {
  return (
    typeof msg === "object" &&
    msg !== null &&
    typeof (msg as { id?: unknown }).id === "string" &&
    typeof (msg as { method?: unknown }).method === "string"
  );
}

export function successResponse(
  id: string,
  result: unknown,
): JsonRpcSuccessResponse {
  return { id, result };
}

export function errorResponse(
  id: string,
  code: JsonRpcErrorCode,
  message: string,
  data?: unknown,
): JsonRpcErrorResponse {
  return { id, error: { code, message, data } };
}
