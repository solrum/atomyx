import type { CompactElement } from "../adapters/device-controller.port.js";
import { requireController, type AdetContext } from "../runtime/adet-context.js";
import type { JsonSchema } from "../types.js";
import {
  FuzzyResourceMatcher,
  StructuralInputFinder,
  UiTreeCache,
} from "./core/index.js";
import { Tool } from "./core/tool.js";
import { renderCompactLine } from "./tree-render.js";

export interface FindElementArgs {
  resourceId?: string;
  contentDesc?: string;
  text?: string;
  labelContains?: string;
  keyword?: string;
  role?: string;
  nth?: number;
  nthOfRole?: number;
  inputField?: boolean;
  all?: boolean;
  limit?: number;
}

export type FindElementResult =
  | { found: true; selector?: Record<string, string>; label?: string; role?: string; center?: { x: number; y: number } | null; strategy?: string; stableId?: string; anchorLabel?: string }
  | { found: false; reason?: string; candidates?: unknown[]; suggestions?: unknown[] }
  | { count: number; tree: string };

/**
 * Unified element query. Seven modes all flow through the same tool:
 *
 *   1. inputField=true          → structural find-input strategy chain
 *   2. nthOfRole                → positional "4th button" query
 *   3. all=true                 → list matches as a compact tree
 *   4. keyword                  → 3-tier quality match (exact → word-boundary → substring)
 *                                across contentDesc → resourceId → label → text
 *   5. resourceId/contentDesc/text/labelContains → strict filter
 *   6. nth                      → disambiguate duplicate candidates
 *   7. fuzzy resourceId fallback → suffix match for partial / non-qualified ids
 *
 * Strategies injected:
 *   - UiTreeCache           — shared with GetUiTreeTool (2s dedupe)
 *   - StructuralInputFinder — inputField mode
 *   - FuzzyResourceMatcher  — fuzzy fallback
 */
export class FindElementTool extends Tool<{
  args: FindElementArgs;
  result: FindElementResult;
}> {
  readonly name = "find_element";
  readonly description =
    "Find element(s) on screen. Pass any combination of: `keyword` (cross-language search " +
    "across resourceId > contentDesc > text), `resourceId` / `contentDesc` / `text` (exact), " +
    "`labelContains` (substring), `role` (filter), `nth` (disambiguate duplicates), " +
    "`nthOfRole` (Nth element of a given role), `inputField: true` (structural find-input " +
    "strategy chain), `all: true` (return list).";
  readonly schema: JsonSchema = {
    type: "object",
    properties: {
      resourceId: { type: "string" },
      contentDesc: { type: "string" },
      text: { type: "string" },
      labelContains: { type: "string" },
      keyword: { type: "string" },
      role: { type: "string" },
      nth: { type: "number" },
      nthOfRole: { type: "number" },
      inputField: { type: "boolean", default: false },
      all: { type: "boolean", default: false },
      limit: { type: "number", default: 20 },
    },
  };

  constructor(
    private readonly cache: UiTreeCache,
    private readonly inputFinder: StructuralInputFinder,
    private readonly fuzzy: FuzzyResourceMatcher,
  ) {
    super();
  }

  async execute(args: FindElementArgs, ctx: AdetContext): Promise<FindElementResult> {
    const ctl = requireController(ctx);

    // Mode 1: structural input-field mode.
    if (args.inputField) {
      return this.findInputField(args, ctl);
    }

    const { elements } = await this.cache.ensureDump(ctl);

    // Mode 2: nthOfRole positional query.
    if (args.nthOfRole != null && args.role) {
      return this.nthOfRole(args.role, args.nthOfRole, elements);
    }

    // Mode 3: all=true list mode.
    if (args.all) {
      const matches = elements.filter(this.makeFilter(args)).slice(0, args.limit ?? 20);
      return { count: matches.length, tree: matches.map((m) => renderCompactLine(m)).join("\n") };
    }

    // Single-match mode: collect candidates either via keyword tiers or the strict filter.
    const candidates = args.keyword
      ? this.keywordCandidates(args, elements)
      : elements.filter(this.makeFilter(args));

    // Disambiguate via nth if multiple.
    if (candidates.length > 0) {
      if (candidates.length > 1 && args.nth == null) {
        return {
          found: false,
          reason:
            `Ambiguous: ${candidates.length} elements match. Use \`nth: 0..${candidates.length - 1}\` ` +
            `or tap({x,y}) directly with the @cx,cy coords.`,
          candidates: candidates.slice(0, 8).map(this.elementSummary),
        };
      }
      const pick = candidates[args.nth ?? 0];
      return this.describeMatch(pick);
    }

    // Fuzzy resourceId fallback (for partial / non-qualified ids).
    if (args.resourceId) {
      const fuzzy = this.fuzzy.match(args.resourceId, elements);
      if (fuzzy.kind === "single") {
        return this.describeMatch(fuzzy.element);
      }
      if (fuzzy.kind === "ambiguous") {
        return {
          found: false,
          reason:
            `Ambiguous: "${args.resourceId}" matches ${fuzzy.candidates.length} elements. ` +
            `Use the FULL resourceId.`,
          candidates: fuzzy.candidates
            .slice(0, 8)
            .map((c) => c.selector?.resourceId)
            .filter(Boolean),
        };
      }
    }

    // Nothing matched — return suggestions by substring of whatever query was passed.
    return this.noMatchWithSuggestions(args, elements);
  }

  // ── helpers ────────────────────────────────────────────────────────────

  private async findInputField(
    args: FindElementArgs,
    ctl: ReturnType<typeof requireController>,
  ): Promise<FindElementResult> {
    const match = await this.inputFinder.find(
      {
        resourceId: args.resourceId,
        contentDesc: args.contentDesc,
        label: args.text,
        keyword: args.keyword ?? args.labelContains,
      },
      ctl,
    );
    if (!match || !match.element.bounds) return { found: false };
    const b = match.element.bounds;
    return {
      found: true,
      strategy: match.strategy,
      stableId: match.stableId?.value,
      anchorLabel: match.anchor.text ?? match.anchor.contentDesc ?? undefined,
      center: {
        x: Math.round((b.left + b.right) / 2),
        y: Math.round((b.top + b.bottom) / 2),
      },
    };
  }

  private nthOfRole(
    role: string,
    index: number,
    elements: CompactElement[],
  ): FindElementResult {
    const sameRole = elements.filter((e) => e.role === role);
    const picked = sameRole[index];
    if (!picked) {
      return {
        found: false,
        reason: `nthOfRole=${index}: only ${sameRole.length} elements with role="${role}"`,
      };
    }
    return this.describeMatch(picked);
  }

  /**
   * 3-tier keyword quality match (exact → word-boundary → substring) across
   * contentDesc → resourceId → label → text. First tier+field combo with
   * matches wins — prevents short keywords like "OK" from substring-matching
   * unrelated ids like "stroke".
   */
  private keywordCandidates(args: FindElementArgs, elements: CompactElement[]): CompactElement[] {
    const kw = (args.keyword ?? "").toLowerCase();
    if (!kw) return [];
    const escaped = kw.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    const wordRe = new RegExp(`(^|[^\\p{L}\\p{N}])${escaped}([^\\p{L}\\p{N}]|$)`, "iu");
    const matchExact = (v?: string) => (v ?? "").toLowerCase() === kw;
    const matchWord = (v?: string) => wordRe.test(v ?? "");
    const matchSub = (v?: string) => (v ?? "").toLowerCase().includes(kw);
    const tiers: Array<(v?: string) => boolean> = [matchExact, matchWord, matchSub];
    const fields: Array<(el: CompactElement) => string | undefined> = [
      (el) => el.selector?.contentDesc,
      (el) => el.selector?.resourceId,
      (el) => el.label,
      (el) => el.selector?.text,
    ];
    for (const tier of tiers) {
      for (const field of fields) {
        const found = elements.filter((el) => {
          if (args.role && el.role !== args.role) return false;
          return tier(field(el));
        });
        if (found.length > 0) return found;
      }
    }
    return [];
  }

  private makeFilter(args: FindElementArgs) {
    const needle = args.labelContains?.toLowerCase();
    return (el: CompactElement) => {
      if (args.role && el.role !== args.role) return false;
      if (args.resourceId && el.selector?.resourceId !== args.resourceId) return false;
      if (args.contentDesc) {
        if (
          el.selector?.contentDesc !== args.contentDesc &&
          el.selector?.text !== args.contentDesc &&
          el.label !== args.contentDesc
        ) return false;
      }
      if (args.text) {
        if (
          el.selector?.contentDesc !== args.text &&
          el.selector?.text !== args.text &&
          el.label !== args.text
        ) return false;
      }
      if (needle && !el.label?.toLowerCase().includes(needle)) return false;
      return true;
    };
  }

  private describeMatch(el: CompactElement): FindElementResult {
    const cx = el.bounds ? Math.round((el.bounds.left + el.bounds.right) / 2) : null;
    const cy = el.bounds ? Math.round((el.bounds.top + el.bounds.bottom) / 2) : null;
    return {
      found: true,
      selector: el.selector,
      label: el.label || undefined,
      role: el.role,
      center: cx != null && cy != null ? { x: cx, y: cy } : null,
    };
  }

  private elementSummary = (el: CompactElement) => {
    const cx = el.bounds ? Math.round((el.bounds.left + el.bounds.right) / 2) : null;
    const cy = el.bounds ? Math.round((el.bounds.top + el.bounds.bottom) / 2) : null;
    return {
      selector: el.selector,
      label: el.label || undefined,
      role: el.role,
      center: cx != null && cy != null ? { x: cx, y: cy } : null,
    };
  };

  private noMatchWithSuggestions(
    args: FindElementArgs,
    elements: CompactElement[],
  ): FindElementResult {
    const queryStr = (args.resourceId ?? args.contentDesc ?? args.text ?? args.keyword ?? "").toLowerCase();
    if (!queryStr) return { found: false };
    const suggestions = elements
      .filter((el) => {
        const r = (el.selector?.resourceId ?? "").toLowerCase();
        const c = (el.selector?.contentDesc ?? "").toLowerCase();
        const t = (el.label ?? "").toLowerCase();
        return r.includes(queryStr) || c.includes(queryStr) || t.includes(queryStr);
      })
      .slice(0, 5)
      .map((el) => el.selector);
    return suggestions.length > 0 ? { found: false, suggestions } : { found: false };
  }
}
