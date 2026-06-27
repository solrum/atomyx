import type { App, Device, RunEvent, RunOpts, UiTreeNode } from "./runtime.types.js";

/**
 * Public contract for Studio's core runtime. The primary adapter
 * (`EmbeddedRuntime` under `platform/`) spawns a Node sidecar that
 * loads `@atomyx/driver` + `@atomyx/script` in-process — zero MCP
 * dependency. A secondary adapter (`McpRuntime`, not yet built)
 * wraps the same contract around the `atomyx-mcp` stdio server
 * for AI-augmented sessions the user opts into.
 *
 * This interface is a candidate public API — third-party tools
 * (VS Code extension, test-mgmt UI) may re-implement it to wire
 * alternative backends. Treat method signatures as stable: a
 * rename or removal breaks every consumer downstream.
 */
export interface StudioRuntime {
  connect(): Promise<void>;
  disconnect(): Promise<void>;

  listDevices(): Promise<readonly Device[]>;
  listApps(deviceId: string): Promise<readonly App[]>;

  /**
   * Execute a YAML script. Returns an async-iterable of events so
   * the caller can render progress live. To cancel, call `stop()` —
   * the iterator will then yield any remaining buffered events and
   * end.
   */
  runScript(yaml: string, opts: RunOpts): AsyncIterable<RunEvent>;

  /**
   * Request cancellation of the in-flight run. Resolves once the
   * stop signal has been delivered to the runtime; the script
   * itself may still take a moment to wind down between steps.
   * No-op when no run is active.
   */
  stop(): Promise<void>;

  screenshot(deviceId: string): Promise<Uint8Array>;

  /**
   * Capture the device's current UI hierarchy as a normalized tree.
   * Snapshot, not live — callers re-invoke when the screen changes.
   * The returned tree follows `UiTreeNode` semantics; platform-
   * specific attribute keys stay under `attributes` and may vary by
   * adapter.
   */
  getUiTree(deviceId: string): Promise<UiTreeNode>;
}
