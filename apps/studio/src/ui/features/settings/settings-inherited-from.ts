import { DEFAULT_ATTRIBUTES } from "../../../domain/features/theme/index.js";
import type {
  AttributeBundle,
  AttributeKey,
  Theme,
  ThemeOverrides,
} from "../../../domain/features/theme/index.js";

export interface InheritanceLayer {
  readonly source:
    | { readonly kind: "override" }
    | { readonly kind: "theme"; readonly themeId: string; readonly label: string }
    | { readonly kind: "default" };
  readonly bundle: AttributeBundle;
}

/**
 * Walk the effective-attribute resolution for a single key and
 * return every layer that contributed — overrides first, then
 * each theme in the extends chain (child → parent), then the
 * built-in defaults. Used by the settings UI to render an
 * "Inherits from" tooltip and the modified indicator.
 */
export function resolveInheritance(
  key: AttributeKey,
  chain: readonly Theme[],
  overrides: ThemeOverrides,
): readonly InheritanceLayer[] {
  const layers: InheritanceLayer[] = [];
  const overrideBundle = overrides[key];
  if (overrideBundle && hasAnyEffect(overrideBundle)) {
    layers.push({ source: { kind: "override" }, bundle: overrideBundle });
  }
  for (const theme of chain) {
    const bundle = theme.attributes[key];
    if (bundle && hasAnyEffect(bundle)) {
      layers.push({
        source: { kind: "theme", themeId: theme.id, label: theme.label },
        bundle,
      });
    }
  }
  layers.push({
    source: { kind: "default" },
    bundle: DEFAULT_ATTRIBUTES[key],
  });
  return layers;
}

function hasAnyEffect(bundle: AttributeBundle): boolean {
  return (
    bundle.foreground !== undefined ||
    bundle.background !== undefined ||
    bundle.fontStyle !== undefined
  );
}

/**
 * Convenience: describe where a specific effect channel of `key`
 * is sourced from, for the "Inherits from" tooltip.
 */
export function sourceForChannel(
  layers: readonly InheritanceLayer[],
  channel: keyof AttributeBundle,
): InheritanceLayer | undefined {
  for (const layer of layers) {
    if (layer.bundle[channel] !== undefined) return layer;
  }
  return undefined;
}
