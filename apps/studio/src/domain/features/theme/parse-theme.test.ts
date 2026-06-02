import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { parseTheme } from "./parse-theme.js";
import { THEME_SCHEMA_VERSION } from "./types.js";

const baseTheme = {
  schemaVersion: THEME_SCHEMA_VERSION,
  id: "test-theme",
  label: "Test",
  monacoBase: "vs-dark",
};

describe("parseTheme", () => {
  it("accepts a minimal valid theme", () => {
    const result = parseTheme({ ...baseTheme, attributes: {} });
    assert.equal(result.ok, true);
  });

  it("accepts declared attributes with all effect channels", () => {
    const result = parseTheme({
      ...baseTheme,
      attributes: {
        ATOMYX_KEYWORD: {
          foreground: "#4ec9b0",
          background: "#2b2b2b",
          fontStyle: "bold",
        },
      },
    });
    assert.equal(result.ok, true);
    if (result.ok) {
      assert.deepEqual(result.theme.attributes["ATOMYX_KEYWORD"], {
        foreground: "#4ec9b0",
        background: "#2b2b2b",
        fontStyle: "bold",
      });
    }
  });

  it("rejects a missing schemaVersion", () => {
    const result = parseTheme({
      id: "x",
      label: "X",
      monacoBase: "vs-dark",
      attributes: {},
    });
    assert.equal(result.ok, false);
  });

  it("rejects a bad monacoBase", () => {
    const result = parseTheme({
      ...baseTheme,
      monacoBase: "vs-banana",
    });
    assert.equal(result.ok, false);
  });

  it("rejects an id that is not lower-kebab-case", () => {
    const result = parseTheme({
      ...baseTheme,
      id: "Bad Id",
    });
    assert.equal(result.ok, false);
  });

  it("rejects non-hex color values", () => {
    const result = parseTheme({
      ...baseTheme,
      attributes: {
        ATOMYX_KEYWORD: { foreground: "teal" },
      },
    });
    assert.equal(result.ok, false);
  });

  it("warns on unknown attribute keys but still parses the rest", () => {
    const result = parseTheme({
      ...baseTheme,
      attributes: {
        ATOMYX_KEYWORD: { foreground: "#4ec9b0" },
        PLUGIN_XYZ_KEY: { foreground: "#ff0000" },
      },
    });
    assert.equal(result.ok, true);
    if (result.ok) {
      assert.ok(result.warnings.some((w) => w.path.includes("PLUGIN_XYZ_KEY")));
      assert.ok(result.theme.attributes["ATOMYX_KEYWORD"]);
    }
  });

  it("warns when schemaVersion is newer than supported", () => {
    const result = parseTheme({
      ...baseTheme,
      schemaVersion: THEME_SCHEMA_VERSION + 1,
      attributes: {},
    });
    assert.equal(result.ok, true);
    if (result.ok) {
      assert.ok(result.warnings.some((w) => w.path === "schemaVersion"));
    }
  });
});
