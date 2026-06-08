import {
  ATTRIBUTE_KEYS,
  type AttributeKey,
  type EffectiveAttributes,
} from "../../../domain/features/theme/index.js";
import { cssVarsFor } from "../../../domain/features/theme/index.js";

/**
 * Write every attribute of an `EffectiveAttributes` map onto the
 * document root as CSS custom properties. Component CSS resolves
 * colors and font effects through these variables, so a theme
 * swap is a single `setProperty` batch — no React re-render, no
 * Monaco re-create.
 *
 * One CSS variable per effect channel:
 *   - foreground   → `--atomyx-keyword`
 *   - background   → `--atomyx-keyword-bg`
 *   - fontStyle    → `--atomyx-keyword-font-weight` +
 *                    `--atomyx-keyword-font-italic`
 *
 * Unset channels receive `""` so `var(--x, fallback)` sites
 * resolve to their fallback.
 */
export function applyTokens(effective: EffectiveAttributes): void {
  const root = document.documentElement;
  for (const key of ATTRIBUTE_KEYS as readonly AttributeKey[]) {
    const bundle = effective[key];
    const vars = cssVarsFor(key);
    root.style.setProperty(vars.foreground, bundle.foreground);
    root.style.setProperty(vars.background, bundle.background ?? "");
    const weight =
      bundle.fontStyle === "bold" || bundle.fontStyle === "bold-italic"
        ? "600"
        : "";
    const italic =
      bundle.fontStyle === "italic" || bundle.fontStyle === "bold-italic"
        ? "italic"
        : "";
    root.style.setProperty(vars.fontWeight, weight);
    root.style.setProperty(vars.fontStyle, italic);
  }
}
