/**
 * Marker type for tool results that should be sent as an MCP
 * `{type: "image"}` content block instead of the default
 * `{type: "text", text: JSON.stringify(...)}`.
 *
 * The server dispatch in `server.ts` checks for `__imageContent`
 * and emits the appropriate content block type.
 */
export interface ImageToolResult {
  readonly __imageContent: true;
  readonly data: string;
  readonly mimeType: string;
}

/** Type guard for image results. */
export function isImageResult(val: unknown): val is ImageToolResult {
  return (
    typeof val === "object" &&
    val !== null &&
    (val as Record<string, unknown>).__imageContent === true &&
    typeof (val as Record<string, unknown>).data === "string" &&
    typeof (val as Record<string, unknown>).mimeType === "string"
  );
}
