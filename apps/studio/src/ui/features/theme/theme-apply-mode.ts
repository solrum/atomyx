import type { Theme } from "../../../domain/features/theme/index.js";

export type SurfaceMode = "dark" | "light";
export type Density = "compact" | "normal" | "comfortable";

export function surfaceModeFor(theme: Theme): SurfaceMode {
  return theme.monacoBase === "vs" || theme.monacoBase === "hc-light"
    ? "light"
    : "dark";
}

/**
 * Set the `data-theme` and `data-density` attributes on `<html>`.
 * The bundle palette in `tokens.css` resolves off these attributes,
 * so calling this aligns the static-token surface layer with the
 * active theme's dark/light polarity.
 */
export function applyThemeMode(theme: Theme, density?: Density): void {
  const root = document.documentElement;
  root.setAttribute("data-theme", surfaceModeFor(theme));
  if (density) {
    root.setAttribute("data-density", density);
  }
}
