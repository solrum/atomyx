import type { CompactElement } from "../../adapters/device-controller.port.js";

export interface FuzzyMatchResult {
  /** Exact / suffix / substring match on a single element. */
  kind: "single";
  element: CompactElement;
  reason: string;
}

export interface FuzzyAmbiguousResult {
  /** Multiple candidates matched — caller must disambiguate. */
  kind: "ambiguous";
  candidates: CompactElement[];
}

export interface FuzzyNoMatchResult {
  kind: "none";
}

export type FuzzyMatch = FuzzyMatchResult | FuzzyAmbiguousResult | FuzzyNoMatchResult;

/**
 * Fuzzy fallback for resourceId queries. Agents frequently pass a partial
 * id (e.g. `G01-05-01/2` without the Android `package:id/` prefix, or
 * just the trailing fragment `2`). Exact lookups in native Android strategy
 * fail on these — the matcher walks the compact tree and tries three
 * progressively broader tiers:
 *
 *   1. exact  — `resourceId === partial`
 *   2. suffix — `resourceId.endsWith('/' + partial)` or `endsWith(partial)`
 *   3. substring — `resourceId.includes(partial)` (loosest)
 *
 * Within each tier, if exactly one candidate matches we return it. If
 * multiple match we stop at that tier and return them as ambiguous — caller
 * forces the agent to disambiguate with a fuller id instead of guessing.
 */
export class FuzzyResourceMatcher {
  match(partial: string, elements: CompactElement[]): FuzzyMatch {
    if (!partial) return { kind: "none" };
    const needle = partial;

    const exact = elements.filter((el) => (el.selector?.resourceId ?? "") === needle);
    if (exact.length === 1) return this.single(exact[0], "exact");
    if (exact.length > 1) return { kind: "ambiguous", candidates: exact };

    const suffix = elements.filter((el) => {
      const r = el.selector?.resourceId ?? "";
      return r !== "" && (r.endsWith(`/${needle}`) || r.endsWith(needle));
    });
    if (suffix.length === 1) return this.single(suffix[0], "suffix");
    if (suffix.length > 1) return { kind: "ambiguous", candidates: suffix };

    const sub = elements.filter((el) => (el.selector?.resourceId ?? "").includes(needle));
    if (sub.length === 1) return this.single(sub[0], "substring");
    if (sub.length > 1) return { kind: "ambiguous", candidates: sub };

    return { kind: "none" };
  }

  private single(element: CompactElement, tier: string): FuzzyMatchResult {
    return {
      kind: "single",
      element,
      reason: `fuzzy ${tier} match → ${element.selector?.resourceId ?? "?"}`,
    };
  }
}
