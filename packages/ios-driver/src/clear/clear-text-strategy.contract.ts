/** Wire response shape for a successful clearFocusedInput command. */
export interface ClearSuccessResponse {
  strategy: string;
}

/**
 * Diagnostic payload encoded in the error message when all four clear strategies
 * are exhausted. Parsed by the TS driver from the JSON string the Swift runner
 * encodes in the `{ok: false, error: "<json>"}` wire response.
 */
export interface ClearFailureDiagnostic {
  strategiesTried: string[];
  lastValue: string | null;
  focusedElementType: string;
  hasHardwareKeyboard: boolean;
}
