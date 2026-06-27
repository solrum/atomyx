export interface ClearTextDiagnostic {
  strategiesTried: string[];
  lastValue: string | null;
  focusedElementType: string;
  hasHardwareKeyboard: boolean;
}

export class ClearTextFailedError extends Error {
  readonly diagnostic: ClearTextDiagnostic;

  constructor(diagnostic: ClearTextDiagnostic) {
    super(
      `clear-text failed after ${diagnostic.strategiesTried.length} strategies: ${diagnostic.strategiesTried.join(", ")}`,
    );
    this.name = "ClearTextFailedError";
    this.diagnostic = diagnostic;
  }
}
