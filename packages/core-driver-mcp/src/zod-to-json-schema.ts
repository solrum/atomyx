import { z } from "zod";

/**
 * Minimal Zod → JSON Schema converter for what MCP's
 * `inputSchema` field expects. NOT a full implementation —
 * covers only the shapes the server-mcp tools actually use:
 * objects, strings, numbers, booleans, arrays, unions,
 * literals, optionals, and nested objects.
 *
 * Why hand-rolled instead of `zod-to-json-schema`: that package
 * is ~30KB and pulls in additional deps. Keeping server-mcp
 * lean — when our tool surface needs a feature this converter
 * doesn't support, we add it here in a few lines.
 *
 * The output shape conforms to JSON Schema draft-07 which is
 * what MCP clients (Claude, etc.) consume.
 */

export type JsonSchema = Record<string, unknown>;

export function zodToJsonSchema(schema: z.ZodType<unknown>): JsonSchema {
  const result = convert(schema);
  // Claude API rejects `anyOf`/`oneOf` at the top level of
  // inputSchema entirely. If a top-level union of objects slips
  // through, flatten it into a single object with all properties
  // optional as a safety net. Tool-level schemas should avoid
  // top-level unions in the first place (use z.object + refine).
  if (!result.type && (result.anyOf || result.oneOf)) {
    const branches = (result.anyOf || result.oneOf) as JsonSchema[];
    if (branches.every((b) => b.type === "object")) {
      const merged: Record<string, JsonSchema> = {};
      for (const branch of branches) {
        const props = (branch.properties ?? {}) as Record<string, JsonSchema>;
        for (const [k, v] of Object.entries(props)) {
          merged[k] = v;
        }
      }
      delete result.anyOf;
      delete result.oneOf;
      result.type = "object";
      result.properties = merged;
    } else {
      result.type = "object";
    }
  }
  return result;
}

function convert(schema: z.ZodType<unknown>): JsonSchema {
  // Unwrap optional: optional(T) → convert(T) with no required marker
  if (schema instanceof z.ZodOptional) {
    return convert(schema.unwrap());
  }
  if (schema instanceof z.ZodDefault) {
    return convert(schema.removeDefault() as z.ZodType<unknown>);
  }
  if (schema instanceof z.ZodNullable) {
    const inner = convert(schema.unwrap());
    return { ...inner, nullable: true };
  }
  if (schema instanceof z.ZodEffects) {
    // refine / transform — convert the inner schema, drop the effect.
    return convert(schema.innerType() as z.ZodType<unknown>);
  }

  // Primitives
  if (schema instanceof z.ZodString) {
    const out: JsonSchema = { type: "string" };
    const desc = (schema as z.ZodString).description;
    if (desc) out.description = desc;
    return out;
  }
  if (schema instanceof z.ZodNumber) {
    const out: JsonSchema = { type: "number" };
    const desc = (schema as z.ZodNumber).description;
    if (desc) out.description = desc;
    return out;
  }
  if (schema instanceof z.ZodBoolean) {
    const out: JsonSchema = { type: "boolean" };
    const desc = (schema as z.ZodBoolean).description;
    if (desc) out.description = desc;
    return out;
  }
  if (schema instanceof z.ZodLiteral) {
    const value = (schema as z.ZodLiteral<unknown>).value;
    return { const: value };
  }

  // Object
  if (schema instanceof z.ZodObject) {
    const shape = (schema as z.ZodObject<z.ZodRawShape>).shape;
    const properties: Record<string, JsonSchema> = {};
    const required: string[] = [];
    for (const key of Object.keys(shape)) {
      const fieldSchema = shape[key]!;
      properties[key] = convert(fieldSchema as z.ZodType<unknown>);
      if (!isOptional(fieldSchema as z.ZodType<unknown>)) {
        required.push(key);
      }
    }
    const out: JsonSchema = {
      type: "object",
      properties,
    };
    if (required.length > 0) out.required = required;
    const desc = (schema as z.ZodObject<z.ZodRawShape>).description;
    if (desc) out.description = desc;
    return out;
  }

  // Array
  if (schema instanceof z.ZodArray) {
    return {
      type: "array",
      items: convert((schema as z.ZodArray<z.ZodType<unknown>>).element),
    };
  }

  // Union → anyOf (NOT oneOf). Zod unions match the first
  // succeeding branch which maps to anyOf semantics ("at least
  // one matches"), not oneOf ("exactly one matches"). More
  // importantly, Claude Code's MCP client silently rejects tool
  // schemas containing `oneOf` at the top level — bisected via
  // A/B testing against a minimal MCP server.
  if (schema instanceof z.ZodUnion) {
    const options = (schema as z.ZodUnion<readonly [z.ZodType<unknown>, ...z.ZodType<unknown>[]]>).options;
    return {
      anyOf: options.map((o) => convert(o)),
    };
  }
  if (schema instanceof z.ZodDiscriminatedUnion) {
    const options = (
      schema as z.ZodDiscriminatedUnion<string, z.ZodObject<z.ZodRawShape>[]>
    ).options;
    return {
      anyOf: options.map((o) => convert(o)),
    };
  }

  if (schema instanceof z.ZodRecord) {
    return {
      type: "object",
      additionalProperties: convert(
        (schema as z.ZodRecord<z.ZodString, z.ZodType<unknown>>).valueSchema,
      ),
    };
  }

  // Fallback: treat as opaque object
  return { type: "object" };
}

function isOptional(schema: z.ZodType<unknown>): boolean {
  if (schema instanceof z.ZodOptional) return true;
  if (schema instanceof z.ZodDefault) return true;
  if (schema instanceof z.ZodEffects) {
    return isOptional(schema.innerType() as z.ZodType<unknown>);
  }
  return false;
}
