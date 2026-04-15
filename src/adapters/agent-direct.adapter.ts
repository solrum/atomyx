import { spawn } from "node:child_process";
import type {
  ActionResult,
  CompactElement,
  DeviceController,
  KeyboardInfo,
  RawElement,
  ResolvedElement,
  Selector,
  TypeKeyboardResult,
} from "./device-controller.port.js";

const AGENT_PORT_ON_DEVICE = 8765;

async function adb(args: string[]): Promise<string> {
  return new Promise((resolve, reject) => {
    const proc = spawn("adb", args);
    let stdout = "";
    let stderr = "";
    proc.stdout.on("data", (chunk) => (stdout += chunk.toString()));
    proc.stderr.on("data", (chunk) => (stderr += chunk.toString()));
    proc.on("close", (code) => {
      if (code === 0) resolve(stdout);
      else reject(new Error(`adb ${args.join(" ")} → ${code}: ${stderr}`));
    });
  });
}

export class AgentDirectController implements DeviceController {
  readonly platform = "android" as const;

  private constructor(
    readonly deviceId: string,
    private readonly hostPort: number,
  ) {}

  static async connect(deviceId: string, hostPort: number): Promise<AgentDirectController> {
    await adb(["-s", deviceId, "forward", `tcp:${hostPort}`, `tcp:${AGENT_PORT_ON_DEVICE}`]);
    const ctl = new AgentDirectController(deviceId, hostPort);
    await ctl.ping();
    return ctl;
  }

  private url(path: string): string {
    return `http://127.0.0.1:${this.hostPort}${path}`;
  }

  private async request<T>(path: string, init?: RequestInit): Promise<T> {
    const res = await fetch(this.url(path), {
      ...init,
      headers: { "content-type": "application/json", ...(init?.headers ?? {}) },
    });
    if (!res.ok) {
      const body = await res.text();
      throw new Error(`agent ${init?.method ?? "GET"} ${path} → ${res.status}: ${body}`);
    }
    if (res.status === 204) return undefined as T;
    return res.json() as Promise<T>;
  }

  private async ping(): Promise<void> {
    await this.request<{ ok: boolean }>("/health");
  }

  // ─── tree / inspection ─────────────────────────────────────────

  getUiTree() {
    return this.request<RawElement>("/tree");
  }

  async getUiSummary(): Promise<CompactElement[]> {
    const res = await this.request<{ elements: CompactElement[] }>("/tree?format=compact");
    return res.elements;
  }

  resolveSelector(selector: Selector): Promise<ResolvedElement> {
    return this.request<ResolvedElement>("/resolve", {
      method: "POST",
      body: JSON.stringify({ selector }),
    });
  }

  screenshot() {
    return this.request<{ base64: string; format: "png" }>("/screenshot");
  }

  getKeyboard() {
    return this.request<KeyboardInfo>("/keyboard");
  }

  // ─── actions ───────────────────────────────────────────────────

  async tap(selector: Selector): Promise<ActionResult> {
    return this.request<ActionResult>("/actions/tap", {
      method: "POST",
      body: JSON.stringify({ selector }),
    });
  }

  async tapCoordinates(x: number, y: number) {
    await this.request("/actions/tap_coords", {
      method: "POST",
      body: JSON.stringify({ x, y }),
    });
  }

  async longPressCoordinates(x: number, y: number, durationMs = 800) {
    await this.request("/actions/long_press", {
      method: "POST",
      body: JSON.stringify({ x, y, durationMs }),
    });
  }

  async clearFocusedInput(): Promise<ActionResult> {
    return await this.request<ActionResult>("/actions/clear_focused_input", {
      method: "POST",
      body: JSON.stringify({}),
    });
  }

  async swipe(fromX: number, fromY: number, toX: number, toY: number, durationMs = 300) {
    await this.request("/actions/swipe", {
      method: "POST",
      body: JSON.stringify({ fromX, fromY, toX, toY, durationMs }),
    });
  }

  async inputText(selector: Selector, text: string): Promise<ActionResult> {
    return this.request<ActionResult>("/actions/input", {
      method: "POST",
      body: JSON.stringify({ selector, text }),
    });
  }

  async typeViaKeyboard(
    text: string,
    perKeyDelayMs?: number,
    clearFirst?: boolean,
  ): Promise<TypeKeyboardResult> {
    return this.request<TypeKeyboardResult>("/actions/type_keyboard", {
      method: "POST",
      body: JSON.stringify({ text, perKeyDelayMs, clearFirst }),
    });
  }

  async pressKey(key: "back" | "home" | "enter"): Promise<ActionResult> {
    await this.request("/actions/key", { method: "POST", body: JSON.stringify({ key }) });
    // Android's KeyEvent dispatch is a system-level intent — it always
    // "fires" regardless of whether the app handles it. We have no
    // per-app consumption signal from the accessibility service, so
    // report ok unconditionally. The guarantee we make here is
    // "the key event was dispatched", not "the app reacted".
    return { ok: true };
  }

  // ─── app ───────────────────────────────────────────────────────
  // Android wire format uses `packageName` / `activity`; this adapter
  // maps to the platform-neutral `appId` / `screen` naming.

  async listApps() {
    const apps = await this.request<{ packageName: string; label?: string }[]>("/apps");
    return apps.map((a) => ({ appId: a.packageName, label: a.label }));
  }

  async launchApp(appId: string) {
    await this.request("/actions/launch", {
      method: "POST",
      body: JSON.stringify({ packageName: appId }),
    });
  }

  async forceStopApp(appId: string) {
    await this.request("/actions/force_stop", {
      method: "POST",
      body: JSON.stringify({ packageName: appId }),
    });
  }

  async currentForeground() {
    const raw = await this.request<{ packageName: string; activity: string }>("/current-activity");
    return { appId: raw.packageName, screen: raw.activity || undefined };
  }

  async dispose() {
    try {
      await adb(["-s", this.deviceId, "forward", "--remove", `tcp:${this.hostPort}`]);
    } catch {
      // best effort
    }
  }
}
