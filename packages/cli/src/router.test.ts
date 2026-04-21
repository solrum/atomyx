import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { modules, shortcuts } from "./router.js";

describe("router — modules", () => {
  it("has driver module registered", () => {
    assert.ok(modules.driver);
    assert.equal(typeof modules.driver.execute, "function");
  });
});

describe("router — shortcuts", () => {
  it("run → driver run", () => {
    const result = shortcuts.run!(["--file", "test.yml"]);
    assert.equal(result.module, "driver");
    assert.deepStrictEqual(result.args, ["run", "--file", "test.yml"]);
  });

  it("devices → driver list-devices", () => {
    const result = shortcuts.devices!(["--json"]);
    assert.equal(result.module, "driver");
    assert.deepStrictEqual(result.args, ["list-devices", "--json"]);
  });

  it("devices without flags", () => {
    const result = shortcuts.devices!([]);
    assert.equal(result.module, "driver");
    assert.deepStrictEqual(result.args, ["list-devices"]);
  });
});
