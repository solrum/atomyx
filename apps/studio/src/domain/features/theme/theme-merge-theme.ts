import { fillDefaults } from "./theme.tokens.js";
import type {
  AttributeBundle,
  AttributeKey,
  EffectiveAttributes,
  Theme,
  ThemeOverrides,
} from "./theme.types.js";

export const MAX_INHERITANCE_DEPTH = 5;

export interface MergeThemeIssue {
  readonly code: "missing-parent" | "cycle" | "depth-exceeded";
  readonly message: string;
}

export type MergeThemeResult =
  | {
      readonly ok: true;
      readonly chain: readonly Theme[];
      readonly attributes: EffectiveAttributes;
      readonly warnings: readonly MergeThemeIssue[];
    }
  | {
      readonly ok: false;
      readonly issues: readonly MergeThemeIssue[];
    };

/**
 * Resolve a theme's effective attribute map. Walks the `extends`
 * chain up to `MAX_INHERITANCE_DEPTH` levels, merges bundles
 * field-wise top-down, applies `overrides` last, then fills any
 * remaining gaps from `DEFAULT_ATTRIBUTES`.
 *
 * Surfaces as issues: unresolvable parent id, cycle, depth
 * exceeded. A missing parent DOES return an issue but the caller
 * MAY still want the partial resolution; we return ok=false for
 * strict correctness — callers can re-invoke with a bare theme
 * (no extends) to degrade gracefully.
 */
export function mergeTheme(
  themeId: string,
  library: ReadonlyMap<string, Theme>,
  overrides: ThemeOverrides = {},
): MergeThemeResult {
  const root = library.get(themeId);
  if (!root) {
    return {
      ok: false,
      issues: [
        {
          code: "missing-parent",
          message: `theme "${themeId}" not found`,
        },
      ],
    };
  }

  const chain: Theme[] = [];
  const seen = new Set<string>();
  let cursor: Theme | undefined = root;
  let depth = 0;

  while (cursor) {
    if (seen.has(cursor.id)) {
      return {
        ok: false,
        issues: [
          {
            code: "cycle",
            message: `cycle detected while resolving "${themeId}" — ${cursor.id} already visited`,
          },
        ],
      };
    }
    seen.add(cursor.id);
    chain.push(cursor);

    if (!cursor.extends) break;
    depth += 1;
    if (depth > MAX_INHERITANCE_DEPTH) {
      return {
        ok: false,
        issues: [
          {
            code: "depth-exceeded",
            message: `inheritance chain for "${themeId}" exceeds the ${MAX_INHERITANCE_DEPTH}-level cap`,
          },
        ],
      };
    }
    const parent = library.get(cursor.extends);
    if (!parent) {
      return {
        ok: false,
        issues: [
          {
            code: "missing-parent",
            message: `theme "${cursor.id}" extends "${cursor.extends}" which is not in the library`,
          },
        ],
      };
    }
    cursor = parent;
  }

  // Merge child-first: the outermost theme declares the final say.
  const merged: Partial<Record<AttributeKey, AttributeBundle>> = {};
  for (let i = chain.length - 1; i >= 0; i--) {
    const layer = chain[i]!.attributes;
    for (const [key, bundle] of Object.entries(layer)) {
      const k = key as AttributeKey;
      merged[k] = mergeBundle(merged[k], bundle);
    }
  }
  for (const [key, bundle] of Object.entries(overrides)) {
    const k = key as AttributeKey;
    merged[k] = mergeBundle(merged[k], bundle);
  }

  return {
    ok: true,
    chain,
    attributes: fillDefaults(merged),
    warnings: [],
  };
}

function mergeBundle(
  existing: AttributeBundle | undefined,
  overlay: AttributeBundle | undefined,
): AttributeBundle {
  return {
    ...(existing ?? {}),
    ...(overlay ?? {}),
  };
}
