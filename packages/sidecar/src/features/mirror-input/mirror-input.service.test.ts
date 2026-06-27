import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { Session } from "../../infra/session/session.js";
import type { DeviceService } from "../device/index.js";
import type { InputService } from "../input/index.js";
import type { IosAgentService } from "../ios-agent/index.js";
import type { AndroidAgentService } from "../android-agent/index.js";
import { MirrorInputService } from "./mirror-input.service.js";
import { DriverNotReadyError, StreamingTouchNotSupportedError } from "./mirror-input.errors.js";

// ---------------------------------------------------------------------------
// Minimal test doubles — only the surface MirrorInputService calls.
// ---------------------------------------------------------------------------

function makeDeviceService(platform: "ios" | "android"): DeviceService {
  return {
    select: async (_id: string) => ({
      id: _id,
      platform,
      name: "test",
      kind: "simulator" as const,
      state: "online" as const,
    }),
    list: async () => [],
    deselect: async () => {},
  } as unknown as DeviceService;
}

function makeInputService(): InputService {
  return {
    tapRatio: async () => {},
    longPressRatio: async () => {},
    swipeRatio: async () => {},
  } as unknown as InputService;
}

function makeIosAgent(
  state: "idle" | "building" | "ready" | "failed",
): IosAgentService {
  return {
    status: (_udid: string) => ({
      udid: _udid,
      state,
      port: 22087,
    }),
  } as unknown as IosAgentService;
}

function makeAndroidAgent(
  state: "idle" | "installing" | "ready" | "failed",
): AndroidAgentService {
  return {
    status: (_serial: string) => ({
      serial: _serial,
      state,
      port: 8765,
    }),
  } as unknown as AndroidAgentService;
}

async function makeSession(
  platform: "ios" | "android",
  deviceId = "test-device",
): Promise<Session> {
  const session = new Session();
  await session.setDevice({
    id: deviceId,
    platform,
    driver: null as never,
    orchestra: null as never,
    dispose: async () => {},
  });
  return session;
}

// ---------------------------------------------------------------------------

describe("MirrorInputService — readiness gate", () => {
  it("tapRatio throws DriverNotReadyError when ios-agent state is 'building'", async () => {
    const deviceId = "UDID-BUILDING";
    const session = await makeSession("ios", deviceId);
    const svc = new MirrorInputService({
      deviceService: makeDeviceService("ios"),
      inputService: makeInputService(),
      session,
      iosAgentService: makeIosAgent("building"),
      androidAgentService: makeAndroidAgent("ready"),
    });

    await assert.rejects(
      () => svc.tapRatio({ deviceId, xRatio: 0.5, yRatio: 0.5 }),
      (err: unknown) => {
        assert.ok(err instanceof DriverNotReadyError, "expected DriverNotReadyError");
        assert.equal((err as DriverNotReadyError).code, "driver-not-ready");
        assert.ok(
          (err as DriverNotReadyError).message.includes("state=building"),
          `message should include state=building, got: ${(err as DriverNotReadyError).message}`,
        );
        return true;
      },
    );
  });

  it("tapRatio throws DriverNotReadyError when android-agent state is 'installing'", async () => {
    const deviceId = "SERIAL-INSTALLING";
    const session = await makeSession("android", deviceId);
    const svc = new MirrorInputService({
      deviceService: makeDeviceService("android"),
      inputService: makeInputService(),
      session,
      iosAgentService: makeIosAgent("ready"),
      androidAgentService: makeAndroidAgent("installing"),
    });

    await assert.rejects(
      () => svc.tapRatio({ deviceId, xRatio: 0.5, yRatio: 0.5 }),
      (err: unknown) => {
        assert.ok(err instanceof DriverNotReadyError, "expected DriverNotReadyError");
        assert.equal((err as DriverNotReadyError).code, "driver-not-ready");
        assert.ok(
          (err as DriverNotReadyError).message.includes("state=installing"),
          `message should include state=installing, got: ${(err as DriverNotReadyError).message}`,
        );
        return true;
      },
    );
  });

  it("tapRatio does not throw when ios-agent state is 'ready'", async () => {
    const deviceId = "UDID-READY";
    const session = await makeSession("ios", deviceId);
    const svc = new MirrorInputService({
      deviceService: makeDeviceService("ios"),
      inputService: makeInputService(),
      session,
      iosAgentService: makeIosAgent("ready"),
      androidAgentService: makeAndroidAgent("idle"),
    });

    await assert.doesNotReject(() =>
      svc.tapRatio({ deviceId, xRatio: 0.5, yRatio: 0.5 }),
    );
  });

  it("tapRatio does not throw when android-agent state is 'ready'", async () => {
    const deviceId = "SERIAL-READY";
    const session = await makeSession("android", deviceId);
    const svc = new MirrorInputService({
      deviceService: makeDeviceService("android"),
      inputService: makeInputService(),
      session,
      iosAgentService: makeIosAgent("idle"),
      androidAgentService: makeAndroidAgent("ready"),
    });

    await assert.doesNotReject(() =>
      svc.tapRatio({ deviceId, xRatio: 0.5, yRatio: 0.5 }),
    );
  });
});

// ---------------------------------------------------------------------------
// Streaming touch — driver capability gate.
// ---------------------------------------------------------------------------

/**
 * Creates a Session with a fake driver whose streaming capability is
 * controlled by the `streamingCapable` flag.
 */
async function makeSessionWithDriver(
  platform: "ios" | "android",
  deviceId: string,
  streamingCapable: boolean,
): Promise<Session> {
  const session = new Session();

  // Build a minimal Driver-shaped object. When streamingCapable is true,
  // add the three streaming methods so isStreamingTouchCapable() returns true.
  const driver: Record<string, unknown> = {
    platform,
    connect: async () => {},
    disconnect: async () => {},
    isConnected: () => false,
    hierarchy: async () => ({}),
    tap: async () => {},
    swipe: async () => {},
    screenshot: async () => new Uint8Array(),
    launchApp: async () => {},
  };

  if (streamingCapable) {
    driver["streamingTouchDown"] = async () => {};
    driver["streamingTouchMove"] = async () => {};
    driver["streamingTouchUp"] = async () => {};
  }

  await session.setDevice({
    id: deviceId,
    platform,
    driver: driver as never,
    orchestra: null as never,
    dispose: async () => {},
  });
  return session;
}

describe("MirrorInputService — streaming touch capability gate", () => {
  it("streamingDown throws StreamingTouchNotSupportedError when driver is not StreamingTouchCapable", async () => {
    const deviceId = "UDID-NO-STREAMING";
    const session = await makeSessionWithDriver("ios", deviceId, false);
    const svc = new MirrorInputService({
      deviceService: makeDeviceService("ios"),
      inputService: makeInputService(),
      session,
      iosAgentService: makeIosAgent("ready"),
      androidAgentService: makeAndroidAgent("idle"),
    });

    await assert.rejects(
      () => svc.streamingDown({ deviceId, xRatio: 0.5, yRatio: 0.5, touchId: 1 }),
      (err: unknown) => {
        assert.ok(
          err instanceof StreamingTouchNotSupportedError,
          `expected StreamingTouchNotSupportedError, got: ${String(err)}`,
        );
        assert.equal(
          (err as StreamingTouchNotSupportedError).code,
          "streaming-touch-not-supported",
        );
        return true;
      },
    );
  });

  it("streamingMove throws StreamingTouchNotSupportedError when driver is not StreamingTouchCapable", async () => {
    const deviceId = "UDID-NO-STREAMING-MOVE";
    const session = await makeSessionWithDriver("ios", deviceId, false);
    const svc = new MirrorInputService({
      deviceService: makeDeviceService("ios"),
      inputService: makeInputService(),
      session,
      iosAgentService: makeIosAgent("ready"),
      androidAgentService: makeAndroidAgent("idle"),
    });

    await assert.rejects(
      () => svc.streamingMove({ deviceId, xRatio: 0.5, yRatio: 0.5, touchId: 1 }),
      (err: unknown) => {
        assert.ok(err instanceof StreamingTouchNotSupportedError);
        assert.equal(
          (err as StreamingTouchNotSupportedError).code,
          "streaming-touch-not-supported",
        );
        return true;
      },
    );
  });

  it("streamingDown resolves when driver implements StreamingTouchCapable", async () => {
    const deviceId = "UDID-WITH-STREAMING";
    const session = await makeSessionWithDriver("ios", deviceId, true);
    const svc = new MirrorInputService({
      deviceService: makeDeviceService("ios"),
      inputService: makeInputService(),
      session,
      iosAgentService: makeIosAgent("ready"),
      androidAgentService: makeAndroidAgent("idle"),
    });

    await assert.doesNotReject(() =>
      svc.streamingDown({ deviceId, xRatio: 0.3, yRatio: 0.7, touchId: 1 }),
    );
  });

  it("streamingDown throws DriverNotReadyError when ios-agent is not ready", async () => {
    const deviceId = "UDID-AGENT-NOT-READY";
    const session = await makeSessionWithDriver("ios", deviceId, true);
    const svc = new MirrorInputService({
      deviceService: makeDeviceService("ios"),
      inputService: makeInputService(),
      session,
      iosAgentService: makeIosAgent("building"),
      androidAgentService: makeAndroidAgent("idle"),
    });

    await assert.rejects(
      () => svc.streamingDown({ deviceId, xRatio: 0.5, yRatio: 0.5, touchId: 1 }),
      (err: unknown) => {
        assert.ok(err instanceof DriverNotReadyError);
        assert.equal((err as DriverNotReadyError).code, "driver-not-ready");
        return true;
      },
    );
  });
});
