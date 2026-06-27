import { describe, it } from "node:test";
import assert from "node:assert/strict";

describe("createIosSimDriver", () => {
  it("returns a Driver-shaped object when hidPort is provided", async () => {
    const { createIosSimDriver } = await import("./index.js");
    const driver = createIosSimDriver({
      kind: "simulator",
      udid: "FAKE-UDID-TEST",
      hidPort: 12345,
    });
    assert.equal(typeof driver.connect, "function");
    assert.equal(typeof driver.disconnect, "function");
    assert.equal(typeof driver.isConnected, "function");
    assert.equal(typeof driver.hierarchy, "function");
    assert.equal(typeof driver.tap, "function");
    assert.equal(typeof driver.swipe, "function");
    assert.equal(typeof driver.screenshot, "function");
    assert.equal(typeof driver.launchApp, "function");
    assert.equal(typeof driver.pressKey, "function");
    assert.equal(driver.platform, "ios");
  });

  it("returns an IosSimDriver instance (not IosDriver)", async () => {
    const { createIosSimDriver } = await import("./index.js");
    const { IosSimDriver } = await import("./ios-sim-driver.impl.js");
    const driver = createIosSimDriver({
      kind: "simulator",
      udid: "FAKE-UDID-TEST",
      hidPort: 12345,
    });
    assert.equal(driver instanceof IosSimDriver, true);
  });

  it("throws when hidPort is missing", async () => {
    const { createIosSimDriver } = await import("./index.js");
    assert.throws(
      () =>
        createIosSimDriver({
          kind: "simulator",
          udid: "FAKE-UDID-TEST",
        }),
      /hidPort/,
    );
  });
});
