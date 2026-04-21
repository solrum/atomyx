/**
 * ScriptArtifacts — collects screenshots and other evidence
 * produced during a script run. The runner provides this to
 * commands; consumers read the collected artifacts after the
 * run completes.
 */
export interface ScriptArtifacts {
  /** Store a screenshot with a label for later retrieval. */
  addScreenshot(label: string, data: Uint8Array): void;
  /** Return all screenshots collected during the run. */
  getScreenshots(): readonly { label: string; data: Uint8Array }[];
}
