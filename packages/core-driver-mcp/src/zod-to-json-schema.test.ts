import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { z } from "zod";
import { zodToJsonSchema } from "./zod-to-json-schema.js";

describe("zodToJsonSchema", () => {
  it("converts a flat object", () => {
    const s = z.object({
      name: z.string(),
      age: z.number(),
    });
    const j = zodToJsonSchema(s);
    assert.equal(j.type, "object");
    const props = j.properties as Record<string, { type: string }>;
    assert.equal(props.name!.type, "string");
    assert.equal(props.age!.type, "number");
    assert.deepEqual(j.required, ["name", "age"]);
  });

  it("marks optional fields as not required", () => {
    const s = z.object({
      a: z.string(),
      b: z.string().optional(),
    });
    const j = zodToJsonSchema(s);
    assert.deepEqual(j.required, ["a"]);
  });

  it("converts arrays", () => {
    const s = z.object({
      items: z.array(z.string()),
    });
    const j = zodToJsonSchema(s);
    const props = j.properties as Record<string, { type: string; items: { type: string } }>;
    assert.equal(props.items!.type, "array");
    assert.equal(props.items!.items.type, "string");
  });

  it("converts unions to anyOf", () => {
    const s = z.object({
      v: z.union([z.string(), z.number()]),
    });
    const j = zodToJsonSchema(s);
    const props = j.properties as Record<string, { anyOf: Array<{ type: string }> }>;
    assert.equal(props.v!.anyOf.length, 2);
    assert.equal(props.v!.anyOf[0]!.type, "string");
    assert.equal(props.v!.anyOf[1]!.type, "number");
  });

  it("preserves description from Zod .describe()", () => {
    const s = z.object({
      x: z.string().describe("the x field"),
    });
    const j = zodToJsonSchema(s);
    const props = j.properties as Record<string, { description: string }>;
    assert.equal(props.x!.description, "the x field");
  });

  it("nested objects", () => {
    const s = z.object({
      outer: z.object({
        inner: z.string(),
      }),
    });
    const j = zodToJsonSchema(s);
    const props = j.properties as Record<
      string,
      { type: string; properties: Record<string, { type: string }> }
    >;
    assert.equal(props.outer!.type, "object");
    assert.equal(props.outer!.properties.inner!.type, "string");
  });

  it("strict objects work", () => {
    const s = z.object({ a: z.string() }).strict();
    const j = zodToJsonSchema(s);
    assert.equal(j.type, "object");
  });

  it("union of object literals flattened to single object (safety net)", () => {
    const s = z.union([
      z.object({ selector: z.object({ id: z.string() }) }),
      z.object({ x: z.number(), y: z.number() }),
    ]);
    const j = zodToJsonSchema(s);
    // Top-level anyOf is forbidden by Claude API — safety net
    // merges all object branches into a single type:"object".
    assert.equal(j.type, "object");
    assert.equal(j.anyOf, undefined);
    const props = j.properties as Record<string, { type: string }>;
    assert.equal(props.selector!.type, "object");
    assert.equal(props.x!.type, "number");
    assert.equal(props.y!.type, "number");
  });

  it("literals become const", () => {
    const s = z.object({
      direction: z.literal("up"),
    });
    const j = zodToJsonSchema(s);
    const props = j.properties as Record<string, { const: string }>;
    assert.equal(props.direction!.const, "up");
  });

  it("refine effects pass through", () => {
    const s = z
      .object({ a: z.string() })
      .refine((v) => v.a.length > 0, "must be non-empty");
    const j = zodToJsonSchema(s);
    assert.equal(j.type, "object");
  });
});
