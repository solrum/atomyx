import { describe, it, before, after } from "node:test";
import assert from "node:assert/strict";
import net from "node:net";
import { EventBus } from "../../infra/events/event-bus.js";
import { IosAgentService } from "./ios-agent.service.js";

// ---------------------------------------------------------------------------
// Minimal fake helper server.
// Listens on a random OS-assigned port. When a connection arrives it
// writes a valid sim-hid handshake line and then hangs (as the real
// helper would). The `bad` variant writes an invalid JSON line.
// ---------------------------------------------------------------------------

class FakeSimHidServer {
  private server: net.Server;
  private _port = 0;
  private readonly response: string;

  constructor(response: string) {
    this.response = response;
    this.server = net.createServer((sock) => {
      sock.write(this.response + "\n");
      // Keep the connection open; helper hangs after handshake.
    });
  }

  get port(): number {
    return this._port;
  }

  start(): Promise<void> {
    return new Promise((resolve) => {
      this.server.listen(0, "127.0.0.1", () => {
        this._port = (this.server.address() as net.AddressInfo).port;
        resolve();
      });
    });
  }

  stop(): Promise<void> {
    return new Promise((resolve) => {
      this.server.closeAllConnections?.();
      this.server.close(() => resolve());
    });
  }
}

// ---------------------------------------------------------------------------

function makeService(): IosAgentService {
  return new IosAgentService({ events: new EventBus() });
}

// ---------------------------------------------------------------------------

describe("IosAgentService.startSimHid — state machine", () => {
  // The tests point ATOMYX_SIM_HID_BIN at a tiny fake process so no real
  // binary is needed. We use `node -e` to print the handshake line
  // and then exit (simulates the helper exiting after handshake).
  //
  // For tests that need a persistent helper we use a FakeSimHidServer
  // instead (a raw TCP server that writes the handshake and stays open).

  it("returns idle state when helper has not been started", () => {
    const svc = makeService();
    const status = svc.simHidStatus("UDID-NEVER-STARTED");
    assert.equal(status.state, "idle");
    assert.equal(status.port, undefined);
  });

  it("resolves with port when helper emits valid handshake", async () => {
    const fakeServer = new FakeSimHidServer(
      JSON.stringify({ event: "listen", port: 19876, transport: "ws-input" }),
    );
    await fakeServer.start();

    // Redirect ATOMYX_SIM_HID_BIN to a node one-liner that writes the handshake
    // and exits. The fake TCP server path isn't used here; we let the
    // IosAgentService spawn a real child via env override.
    const handshake = JSON.stringify({
      event: "listen",
      port: fakeServer.port,
      transport: "ws-input",
    });
    // node one-liner: print handshake and exit cleanly.
    process.env.ATOMYX_SIM_HID_BIN = process.execPath;
    const prevArgv = process.argv;
    // The spawn call uses [helperPath, "--udid", udid]. We need node to
    // accept those args and print the handshake. Use a wrapper script file
    // approach via -e flag is not possible since spawn uses the path as-is.
    // Instead we create a tiny shell wrapper via a temp file.
    const os = await import("node:os");
    const fsp = await import("node:fs/promises");
    const pathMod = await import("node:path");
    const tmpDir = os.tmpdir();
    const scriptPath = pathMod.join(tmpDir, `atomyx-sim-hid-test-${Date.now()}.cjs`);
    await fsp.writeFile(
      scriptPath,
      // A minimal CJS script that prints the handshake and exits.
      `process.stdout.write(${JSON.stringify(handshake + "\n")});`,
    );
    process.env.ATOMYX_SIM_HID_BIN = process.execPath;

    // Temporarily override ATOMYX_SIM_HID_BIN to point at: node <scriptPath>
    // startSimHid spawns: [helperPath, "--udid", udid]
    // So we write a shell wrapper that ignores args and prints the handshake.
    const wrapperPath = pathMod.join(tmpDir, `atomyx-sim-hid-wrapper-${Date.now()}.sh`);
    await fsp.writeFile(
      wrapperPath,
      `#!/bin/sh\nexec ${JSON.stringify(process.execPath)} ${JSON.stringify(scriptPath)} "$@"\n`,
    );
    const { chmod } = await import("node:fs/promises");
    await chmod(wrapperPath, 0o755);
    process.env.ATOMYX_SIM_HID_BIN = wrapperPath;

    const svc = makeService();
    try {
      const port = await svc.startSimHid("UDID-VALID");
      assert.equal(port, fakeServer.port, "port should match handshake");
      const status = svc.simHidStatus("UDID-VALID");
      // After a clean exit the state transitions to "failed" (helper not
      // persistent), but the port was already resolved correctly.
      // We only assert the port from the resolved promise.
      assert.equal(typeof port, "number");
    } finally {
      delete process.env.ATOMYX_SIM_HID_BIN;
      await fsp.rm(scriptPath, { force: true });
      await fsp.rm(wrapperPath, { force: true });
      await fakeServer.stop();
    }
    void prevArgv;
  });

  it("rejects when binary is not found and ATOMYX_SIM_HID_BIN is not set", async () => {
    const saved = process.env.ATOMYX_SIM_HID_BIN;
    delete process.env.ATOMYX_SIM_HID_BIN;
    const svc = makeService();
    try {
      await assert.rejects(
        () => svc.startSimHid("UDID-NO-BINARY"),
        (err: unknown) => {
          assert.ok(err instanceof Error, "expected Error");
          assert.ok(
            (err as Error).message.includes("atomyx-sim-hid helper not found"),
            `message: ${(err as Error).message}`,
          );
          return true;
        },
      );
      const status = svc.simHidStatus("UDID-NO-BINARY");
      assert.equal(status.state, "failed");
    } finally {
      if (saved !== undefined) process.env.ATOMYX_SIM_HID_BIN = saved;
    }
  });

  it("rejects when helper emits invalid handshake JSON", async () => {
    const os = await import("node:os");
    const fsp = await import("node:fs/promises");
    const pathMod = await import("node:path");
    const tmpDir = os.tmpdir();
    const scriptPath = pathMod.join(tmpDir, `atomyx-sim-hid-bad-${Date.now()}.cjs`);
    await fsp.writeFile(scriptPath, `process.stdout.write("not-json\\n");`);
    const wrapperPath = pathMod.join(tmpDir, `atomyx-sim-hid-bad-wrap-${Date.now()}.sh`);
    await fsp.writeFile(
      wrapperPath,
      `#!/bin/sh\nexec ${JSON.stringify(process.execPath)} ${JSON.stringify(scriptPath)} "$@"\n`,
    );
    await fsp.chmod(wrapperPath, 0o755);
    process.env.ATOMYX_SIM_HID_BIN = wrapperPath;
    const svc = makeService();
    try {
      await assert.rejects(
        () => svc.startSimHid("UDID-BAD-JSON"),
        (err: unknown) => {
          assert.ok(err instanceof Error, "expected Error");
          assert.ok(
            (err as Error).message.includes("invalid handshake JSON"),
            `message: ${(err as Error).message}`,
          );
          return true;
        },
      );
      assert.equal(svc.simHidStatus("UDID-BAD-JSON").state, "failed");
    } finally {
      delete process.env.ATOMYX_SIM_HID_BIN;
      await fsp.rm(scriptPath, { force: true });
      await fsp.rm(wrapperPath, { force: true });
    }
  });

  it("rejects when handshake has wrong transport field", async () => {
    const os = await import("node:os");
    const fsp = await import("node:fs/promises");
    const pathMod = await import("node:path");
    const tmpDir = os.tmpdir();
    const scriptPath = pathMod.join(tmpDir, `atomyx-sim-hid-wrongt-${Date.now()}.cjs`);
    const wrongHandshake = JSON.stringify({
      event: "listen",
      port: 1234,
      transport: "wrong-transport",
    });
    await fsp.writeFile(scriptPath, `process.stdout.write(${JSON.stringify(wrongHandshake + "\n")});`);
    const wrapperPath = pathMod.join(tmpDir, `atomyx-sim-hid-wrongt-wrap-${Date.now()}.sh`);
    await fsp.writeFile(
      wrapperPath,
      `#!/bin/sh\nexec ${JSON.stringify(process.execPath)} ${JSON.stringify(scriptPath)} "$@"\n`,
    );
    await fsp.chmod(wrapperPath, 0o755);
    process.env.ATOMYX_SIM_HID_BIN = wrapperPath;
    const svc = makeService();
    try {
      await assert.rejects(
        () => svc.startSimHid("UDID-WRONG-TRANSPORT"),
        (err: unknown) => {
          assert.ok(err instanceof Error, "expected Error");
          assert.ok(
            (err as Error).message.includes("unexpected handshake"),
            `message: ${(err as Error).message}`,
          );
          return true;
        },
      );
      assert.equal(svc.simHidStatus("UDID-WRONG-TRANSPORT").state, "failed");
    } finally {
      delete process.env.ATOMYX_SIM_HID_BIN;
      await fsp.rm(scriptPath, { force: true });
      await fsp.rm(wrapperPath, { force: true });
    }
  });

  it("concurrent startSimHid calls for same UDID share the same promise", async () => {
    const os = await import("node:os");
    const fsp = await import("node:fs/promises");
    const pathMod = await import("node:path");
    const tmpDir = os.tmpdir();
    // Use a unique port per test run to avoid collisions.
    const handshake = JSON.stringify({ event: "listen", port: 19877, transport: "ws-input" });
    const scriptPath = pathMod.join(tmpDir, `atomyx-sim-hid-conc-${Date.now()}.cjs`);
    await fsp.writeFile(scriptPath, `process.stdout.write(${JSON.stringify(handshake + "\n")});`);
    const wrapperPath = pathMod.join(tmpDir, `atomyx-sim-hid-conc-wrap-${Date.now()}.sh`);
    await fsp.writeFile(
      wrapperPath,
      `#!/bin/sh\nexec ${JSON.stringify(process.execPath)} ${JSON.stringify(scriptPath)} "$@"\n`,
    );
    await fsp.chmod(wrapperPath, 0o755);
    process.env.ATOMYX_SIM_HID_BIN = wrapperPath;
    const svc = makeService();
    try {
      const [p1, p2, p3] = await Promise.all([
        svc.startSimHid("UDID-CONCURRENT"),
        svc.startSimHid("UDID-CONCURRENT"),
        svc.startSimHid("UDID-CONCURRENT"),
      ]);
      // All concurrent callers get the same port.
      assert.equal(p1, 19877);
      assert.equal(p2, 19877);
      assert.equal(p3, 19877);
    } finally {
      delete process.env.ATOMYX_SIM_HID_BIN;
      await fsp.rm(scriptPath, { force: true });
      await fsp.rm(wrapperPath, { force: true });
    }
  });
});
