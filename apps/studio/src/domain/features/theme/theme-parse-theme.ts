import { z } from "zod";
import {
  ATTRIBUTE_KEY_SET,
  FONT_STYLES,
  MONACO_BASE_THEMES,
  THEME_SCHEMA_VERSION,
  type AttributeBundle,
  type AttributeKey,
  type Theme,
} from "./theme.types.js";

export interface ThemeParseIssue {
  readonly path: string;
  readonly message: string;
}

export type ThemeParseResult =
  | { readonly ok: true; readonly theme: Theme; readonly warnings: readonly ThemeParseIssue[] }
  | { readonly ok: false; readonly issues: readonly ThemeParseIssue[] };

const HEX = z.string().regex(/^#[0-9a-fA-F]{6}$/, "expected #rrggbb");

const AttributeBundleSchema = z
  .object({
    foreground: HEX.optional(),
    background: HEX.optional(),
    fontStyle: z.enum(FONT_STYLES).optional(),
  })
  .strict();

const ThemeSchema = z
  .object({
    schemaVersion: z
      .number()
      .int()
      .positive()
      .describe(
        "Attribute-catalogue schema version. This build accepts `" +
          String(THEME_SCHEMA_VERSION) +
          "`.",
      ),
    id: z
      .string()
      .min(1)
      .regex(
        /^[a-z0-9][a-z0-9-]*[a-z0-9]$/,
        "theme id must be lower-kebab-case (e.g. `intellij-darcula`)",
      ),
    label: z.string().min(1),
    extends: z.string().optional(),
    monacoBase: z.enum(MONACO_BASE_THEMES),
    attributes: z.record(z.string(), AttributeBundleSchema).default({}),
  })
  .strict();

/**
 * Parse and validate a theme JSON payload. Unknown attribute keys
 * are returned as warnings (plugin-namespaced keys may legitimately
 * appear from future plugins), while shape / schemaVersion
 * mismatches are rejected with issues.
 */
export function parseTheme(raw: unknown): ThemeParseResult {
  const result = ThemeSchema.safeParse(raw);
  if (!result.success) {
    return {
      ok: false,
      issues: result.error.issues.map((i) => ({
        path: i.path.length === 0 ? "<root>" : i.path.join("."),
        message: i.message,
      })),
    };
  }

  const warnings: ThemeParseIssue[] = [];
  if (result.data.schemaVersion > THEME_SCHEMA_VERSION) {
    warnings.push({
      path: "schemaVersion",
      message:
        "theme declares a newer schemaVersion than this Studio build supports; unknown attributes fall back to defaults",
    });
  }

  const attributes: Partial<Record<AttributeKey, AttributeBundle>> = {};
  for (const [key, bundle] of Object.entries(result.data.attributes)) {
    if (!ATTRIBUTE_KEY_SET.has(key)) {
      warnings.push({
        path: `attributes.${key}`,
        message:
          "unknown attribute key — ignored. Valid keys live in domain/theme/types.ts.",
      });
      continue;
    }
    attributes[key as AttributeKey] = bundle;
  }

  const theme: Theme = {
    schemaVersion: result.data.schemaVersion,
    id: result.data.id,
    label: result.data.label,
    ...(result.data.extends !== undefined
      ? { extends: result.data.extends }
      : {}),
    monacoBase: result.data.monacoBase,
    attributes,
  };

  return { ok: true, theme, warnings };
}
