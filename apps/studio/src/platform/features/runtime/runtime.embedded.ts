import { invoke, Channel } from "@tauri-apps/api/core";
import type { StudioRuntime } from "../../../domain/features/runtime/index.js";
import type {
  App,
  Device,
  RunEvent,
  RunOpts,
  StepToken,
  UiTreeNode,
} from "../../../domain/features/runtime/index.js";

/**
 * Primary `StudioRuntime` adapter. Delegates to the Tauri Rust
 * backend, which owns a long-lived Node sidecar that loads
 * `@atomyx/driver` + `@atomyx/script` in-process. JSON commands
 * travel over stdio; run events stream back via a Tauri Channel.
 *
 * Keeping the protocol in Rust (not in the browser context)
 * avoids shipping a Node-style process API to the renderer and
 * keeps memory + permissions predictable.
 *
 * MCP is NOT involved on this path. AI-agent sessions get their
 * own secondary adapter (`McpRuntime`, not yet implemented) that
 * users opt into via a settings toggle — Studio's core path does
 * not depend on MCP.
 */
export class EmbeddedRuntime implements StudioRuntime {
  async connect(): Promise<void> {
    await invoke("runtime_connect");
  }

  async disconnect(): Promise<void> {
    await invoke("runtime_disconnect");
  }

  async listDevices(): Promise<readonly Device[]> {
    return invoke<readonly Device[]>("runtime_list_devices");
  }

  async listApps(deviceId: string): Promise<readonly App[]> {
    return invoke<readonly App[]>("runtime_list_apps", { deviceId });
  }

  async *runScript(
    yaml: string,
    opts: RunOpts,
  ): AsyncIterable<RunEvent> {
    // Wire events arrive as { event: string, payload: {...} } from
    // the sidecar's EventBus via the Rust broadcast channel. Convert
    // them into the typed RunEvent the caller expects.
    //
    // The Rust `runtime_run_script` command does not resolve until
    // the underlying `sidecar.call("runScript")` JSON-RPC returns —
    // i.e. after every step has finished. Awaiting `invoke()` here
    // would defer every yield until the run is over, which is the
    // shape that makes the UI show only the final result. Instead
    // the invoke runs in the background while the iterator drains
    // events as the channel delivers them.
    const channel = new Channel<WireEvent>();
    const buffer: RunEvent[] = [];
    let pendingResolve: (() => void) | null = null;
    let finished = false;
    let invokeError: unknown = null;

    channel.onmessage = (wire) => {
      const translated = translateWireEvent(wire);
      if (!translated) return;
      buffer.push(translated);
      pendingResolve?.();
      pendingResolve = null;
      if (translated.type === "runCompleted") {
        finished = true;
      }
    };

    const invokePromise = invoke<void>("runtime_run_script", {
      yaml,
      opts,
      cwd: opts.cwd ?? null,
      onEvent: channel,
    }).catch((err: unknown) => {
      invokeError = err;
      finished = true;
      pendingResolve?.();
      pendingResolve = null;
    });

    try {
      while (true) {
        if (buffer.length > 0) {
          const next = buffer.shift()!;
          yield next;
          if (next.type === "runCompleted") return;
          continue;
        }
        if (finished) {
          if (invokeError) throw invokeError;
          return;
        }
        await new Promise<void>((resolve) => {
          pendingResolve = resolve;
        });
      }
    } finally {
      await invokePromise;
    }
  }

  async stop(): Promise<void> {
    await invoke("runtime_stop_script");
  }

  async screenshot(deviceId: string): Promise<Uint8Array> {
    const numbers = await invoke<readonly number[]>("runtime_screenshot", {
      deviceId,
    });
    return Uint8Array.from(numbers);
  }

  async getUiTree(deviceId: string): Promise<UiTreeNode> {
    return invoke<UiTreeNode>("runtime_get_ui_tree", { deviceId });
  }
}

interface WireEvent {
  readonly event: string;
  readonly payload: Record<string, unknown>;
}

function translateWireEvent(wire: WireEvent): RunEvent | null {
  const { event, payload } = wire;
  switch (event) {
    case "runStarted":
      return {
        type: "runStarted",
        runId: String(payload.runId ?? ""),
        startedAt: Number(payload.startedAt ?? Date.now()),
      };
    case "stepStarted":
      return {
        type: "stepStarted",
        stepIndex: Number(payload.stepIndex ?? 0),
        command: String(payload.command ?? ""),
        summary: String(payload.summary ?? payload.command ?? ""),
        tokens: parseTokens(payload.tokens, String(payload.command ?? "")),
        depth: Number(payload.depth ?? 0),
        line:
          typeof payload.line === "number" && payload.line > 0
            ? payload.line
            : undefined,
      };
    case "stepCompleted":
      return {
        type: "stepCompleted",
        stepIndex: Number(payload.stepIndex ?? 0),
        command: String(payload.command ?? ""),
        ok: Boolean(payload.ok),
        durationMs: Number(payload.durationMs ?? 0),
        summary: String(payload.summary ?? payload.command ?? ""),
        tokens: parseTokens(payload.tokens, String(payload.command ?? "")),
        detail: payload.detail ? String(payload.detail) : undefined,
        depth: Number(payload.depth ?? 0),
        line:
          typeof payload.line === "number" && payload.line > 0
            ? payload.line
            : undefined,
      };
    case "runCompleted":
      return {
        type: "runCompleted",
        ok: Boolean(payload.ok),
        completedAt: Date.now(),
        failedAtStep:
          payload.failedAtStep !== undefined
            ? Number(payload.failedAtStep)
            : undefined,
      };
    case "scenarioStarted":
      return {
        type: "scenarioStarted",
        scenarioName: String(payload.scenarioName ?? ""),
        totalScripts: Number(payload.totalScripts ?? 0),
      };
    case "scriptStarted":
      return {
        type: "scriptStarted",
        scriptIndex: Number(payload.scriptIndex ?? 0),
        scriptPath: String(payload.scriptPath ?? ""),
      };
    case "scriptCompleted": {
      const status = payload.status;
      const ok =
        status === "passed" ||
        status === "failed" ||
        status === "skipped" ||
        status === "errored"
          ? (status as "passed" | "failed" | "skipped" | "errored")
          : "errored";
      return {
        type: "scriptCompleted",
        scriptIndex: Number(payload.scriptIndex ?? 0),
        scriptPath: String(payload.scriptPath ?? ""),
        status: ok,
        durationMs: Number(payload.durationMs ?? 0),
        failedAtStep:
          payload.failedAtStep !== undefined
            ? Number(payload.failedAtStep)
            : undefined,
      };
    }
    case "scenarioCompleted":
      return {
        type: "scenarioCompleted",
        ok: Boolean(payload.ok),
        totalScripts: Number(payload.totalScripts ?? 0),
        passedScripts: Number(payload.passedScripts ?? 0),
        durationMs: Number(payload.durationMs ?? 0),
      };
    case "runErrored":
      return {
        type: "consoleLog",
        level: "error",
        line: String(payload.message ?? "run errored"),
        at: Date.now(),
      };
    default:
      return null;
  }
}

const TOKEN_KINDS: ReadonlySet<string> = new Set([
  "keyword",
  "identifier",
  "string",
  "punct",
  "mask",
]);

function parseTokens(value: unknown, fallbackKeyword: string): readonly StepToken[] {
  if (!Array.isArray(value)) {
    return fallbackKeyword
      ? [{ kind: "keyword", text: fallbackKeyword }]
      : [];
  }
  const out: StepToken[] = [];
  for (const raw of value) {
    if (raw && typeof raw === "object") {
      const r = raw as { kind?: unknown; text?: unknown };
      const kind = typeof r.kind === "string" && TOKEN_KINDS.has(r.kind) ? r.kind : "string";
      const text = typeof r.text === "string" ? r.text : "";
      if (text) out.push({ kind: kind as StepToken["kind"], text });
    }
  }
  return out;
}
