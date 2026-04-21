import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { isImageResult } from "./tool-result.js";

describe("isImageResult", () => {
  it("returns true for valid image result", () => {
    assert.equal(
      isImageResult({ __imageContent: true, data: "abc", mimeType: "image/png" }),
      true,
    );
  });

  it("returns false for plain object", () => {
    assert.equal(isImageResult({ ok: true, base64: "abc" }), false);
  });

  it("returns false for null", () => {
    assert.equal(isImageResult(null), false);
  });

  it("returns false when data is not a string", () => {
    assert.equal(
      isImageResult({ __imageContent: true, data: 123, mimeType: "image/png" }),
      false,
    );
  });
});
