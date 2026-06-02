import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { z } from "zod";
import {
  ROUTES,
  WIRE_PROTOCOL_VERSION,
  parseRequest,
  parseResponse,
} from "./routes.schema.js";
import { TreeNodeSchema } from "./tree-node.schema.js";

describe("wire protocol routes", () => {
  it("every route has method + path + request + response", () => {
    for (const [name, route] of Object.entries(ROUTES)) {
      assert.ok(route.method === "GET" || route.method === "POST", `${name}: method`);
      assert.ok(route.path.startsWith("/"), `${name}: path must start with /`);
      assert.ok(route.request instanceof z.ZodType, `${name}: request is Zod`);
      assert.ok(route.response instanceof z.ZodType, `${name}: response is Zod`);
    }
  });

  it("path is unique across routes", () => {
    const paths = new Set<string>();
    for (const route of Object.values(ROUTES)) {
      assert.equal(paths.has(route.path), false, `duplicate path: ${route.path}`);
      paths.add(route.path);
    }
  });

  it("WIRE_PROTOCOL_VERSION is semver-ish", () => {
    assert.match(WIRE_PROTOCOL_VERSION, /^\d+\.\d+$/);
  });
});

describe("parseRequest / parseResponse", () => {
  it("validates a tap request", () => {
    const parsed = parseRequest("tap", { x: 100, y: 200 });
    assert.deepEqual(parsed, { x: 100, y: 200 });
  });

  it("rejects a tap request missing coordinates", () => {
    assert.throws(() => parseRequest("tap", { x: 100 }));
  });

  it("validates a swipe request", () => {
    const parsed = parseRequest("swipe", {
      from: { x: 10, y: 20 },
      to: { x: 30, y: 40 },
      durationMs: 200,
    });
    assert.equal(parsed.durationMs, 200);
  });

  it("rejects a swipe request with zero duration", () => {
    assert.throws(() =>
      parseRequest("swipe", {
        from: { x: 10, y: 20 },
        to: { x: 30, y: 40 },
        durationMs: 0,
      }),
    );
  });

  it("validates a launchApp request with optional args", () => {
    const parsed = parseRequest("launchApp", {
      appId: "com.example.app",
      args: ["--debug"],
    });
    assert.equal(parsed.appId, "com.example.app");
  });

  it("validates a health response", () => {
    const parsed = parseResponse("health", {
      ok: true,
      version: "1.0",
      capabilities: {
        canScreenshot: true,
        canEraseText: true,
        canWaitForIdle: false,
        canSetLocation: false,
        canSetOrientation: false,
        supportedKeyCodes: ["back", "home", "enter"],
      },
    });
    assert.equal(parsed.ok, true);
  });

  it("validates a hierarchy response with nested tree", () => {
    const parsed = parseResponse("hierarchy", {
      tree: {
        attributes: { id: "root", role: "container" },
        children: [
          {
            attributes: { id: "btn", role: "button", text: "Login" },
            children: [],
            clickable: true,
            enabled: true,
          },
        ],
      },
    });
    assert.equal(parsed.tree.children.length, 1);
    assert.equal(parsed.tree.children[0]!.clickable, true);
  });

  it("rejects a tree node with non-string attribute values", () => {
    assert.throws(() =>
      parseResponse("hierarchy", {
        tree: {
          attributes: { x: 123 },
          children: [],
        },
      }),
    );
  });

  it("validates a pressKey response with ok:false + reason", () => {
    const parsed = parseResponse("pressKey", {
      ok: false,
      reason: "no system back on iOS — use find_element on Close",
    });
    assert.equal(parsed.ok, false);
  });

  it("validates deviceInfo response", () => {
    const parsed = parseResponse("deviceInfo", {
      platform: "android",
      platformVersion: "14",
      model: "Pixel 8",
      udid: "abc123",
      kind: "device",
    });
    assert.equal(parsed.kind, "device");
  });

  it("rejects deviceInfo with an invalid kind", () => {
    assert.throws(() =>
      parseResponse("deviceInfo", {
        platform: "android",
        platformVersion: "14",
        model: "Pixel 8",
        udid: "abc123",
        kind: "phone", // not in union
      }),
    );
  });
});

describe("TreeNodeSchema — recursive", () => {
  it("accepts a deeply nested tree", () => {
    const deep = {
      attributes: { id: "a" },
      children: [
        {
          attributes: { id: "b" },
          children: [
            {
              attributes: { id: "c" },
              children: [],
            },
          ],
        },
      ],
    };
    const parsed = TreeNodeSchema.parse(deep);
    assert.equal(parsed.children[0]!.children[0]!.attributes.id, "c");
  });

  it("requires attributes to be a string map", () => {
    assert.throws(() =>
      TreeNodeSchema.parse({
        attributes: { id: 42 },
        children: [],
      }),
    );
  });

  it("accepts state booleans as optional", () => {
    const parsed = TreeNodeSchema.parse({
      attributes: { id: "x" },
      children: [],
    });
    assert.equal(parsed.clickable, undefined);
    assert.equal(parsed.enabled, undefined);
  });

  it("preserves the full state-boolean set including visible", () => {
    const parsed = TreeNodeSchema.parse({
      attributes: { id: "x" },
      children: [],
      clickable: true,
      enabled: true,
      focused: false,
      selected: true,
      checked: false,
      visible: true,
    });
    assert.equal(parsed.visible, true);
    assert.equal(parsed.selected, true);
    assert.equal(parsed.checked, false);
  });
});
