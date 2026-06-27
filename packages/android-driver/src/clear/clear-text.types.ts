/**
 * Diagnostic payload included in the error when all four clear-text
 * strategies fail. Populated by the APK and surfaced here for
 * structured error reporting.
 */
export interface ClearTextDiagnostic {
  readonly strategiesTried: readonly string[];
  readonly lastValue: string;
  readonly focusedNodeDesc: string;
  readonly screenWidth: number;
  readonly screenHeight: number;
}

/** Error thrown by eraseText when the APK exhausts all clear-text strategies. */
export class ClearTextError extends Error {
  readonly diagnostic: ClearTextDiagnostic;

  constructor(diagnostic: ClearTextDiagnostic) {
    super(
      `Android clear-text failed after all strategies: ` +
        `tried=[${diagnostic.strategiesTried.join(",")}] ` +
        `lastValue="${diagnostic.lastValue}"`,
    );
    this.name = "ClearTextError";
    this.diagnostic = diagnostic;
  }
}
