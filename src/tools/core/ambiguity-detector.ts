import type { CompactElement } from "../../adapters/device-controller.port.js";

/**
 * Detects duplicate selectors in a compact tree dump and annotates them
 * so agents know a selector alone is not enough — they must disambiguate
 * via coordinates or `nth`.
 *
 * Example: two elements both report `contentDesc="注文"` (the header title
 * AND the bottom nav tab). A naive `tap({contentDesc: "注文"})` picks the
 * first one, which may be wrong.
 *
 * The detector computes a duplicate count keyed by the BEST selector token
 * (resourceId > contentDesc > text > role) so agents see `(2×)` next to
 * any row whose token is not unique.
 */
export class AmbiguityDetector {
  /**
   * Compute a map from selector-token-key → count of elements sharing it.
   * Use with `renderCompactLine(e, dupCounts.get(this.tokenOf(e)))`.
   */
  computeDuplicateCounts(elements: CompactElement[]): Map<string, number> {
    const counts = new Map<string, number>();
    for (const e of elements) {
      const k = this.tokenOf(e);
      counts.set(k, (counts.get(k) ?? 0) + 1);
    }
    return counts;
  }

  /**
   * Token key in stability order: resourceId > contentDesc > text > role.
   * Mirror the token chosen by `renderCompactLine` so the count aligns
   * with the displayed form.
   */
  tokenOf(e: CompactElement): string {
    const sel = e.selector ?? {};
    if (sel.resourceId) return `r:${sel.resourceId}`;
    if (sel.contentDesc) return `c:${sel.contentDesc}`;
    if (sel.text) return `t:${sel.text}`;
    return `role:${e.role}`;
  }
}
