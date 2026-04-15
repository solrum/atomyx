import type {
  ActionResult,
  ResolvedElement,
  Selector,
} from "../adapters/device-controller.port.js";
import { requireController, type AtomyxContext } from "../runtime/atomyx-context.js";
import type { JsonSchema } from "../types.js";
import {
  FuzzyResourceMatcher,
  ImeGeometricGuard,
  SelectorResolutionPipeline,
} from "./core/index.js";
import { Tool } from "./core/tool.js";
import { checkSelectorQuality } from "./selector-quality.js";

const selectorSchema: JsonSchema = {
  type: "object",
  properties: {
    resourceId: { type: "string" },
    contentDesc: { type: "string" },
    text: { type: "string" },
    textContains: { type: "string" },
    hint: { type: "string" },
    nth: { type: "number", default: 0 },
  },
};

export interface TapArgs {
  selector?: Selector;
  x?: number;
  y?: number;
}

export interface TapResult extends ActionResult {
  candidates?: (string | undefined)[];
  isInIme?: boolean;
  blockedSelector?: Selector;
  selectorWarning?: string;
  resolvedBounds?: { left: number; top: number; right: number; bottom: number };
  resolvedRole?: string | null;
  selectorTried?: Selector;
}

/**
 * Tap a local-state element. Orchestrates four injected strategies:
 *
 *   1. ImeGeometricGuard    — refuse coord taps inside the soft keyboard
 *   2. SelectorResolutionPipeline — priority-ordered broadening
 *   3. FuzzyResourceMatcher — suffix match for partial / non-qualified ids
 *   4. selector quality check — warn if agent used text when a stable id exists
 *
 * The handler is intentionally short: orchestration only. Every rule lives
 * in a strategy class with its own unit tests.
 */
export class TapTool extends Tool<{ args: TapArgs; result: TapResult }> {
  readonly name = "tap";
  readonly description =
    "Tap an element. Pass `selector` OR `{x,y}` coordinates. " +
    "For navigation/submit/login use tap_and_wait_transition instead. See get_playbook.";
  readonly schema: JsonSchema = {
    type: "object",
    properties: {
      selector: selectorSchema,
      x: { type: "number" },
      y: { type: "number" },
    },
  };

  constructor(
    private readonly resolver: SelectorResolutionPipeline,
    private readonly imeGuard: ImeGeometricGuard,
    private readonly fuzzy: FuzzyResourceMatcher,
  ) {
    super();
  }

  async execute(args: TapArgs, ctx: AtomyxContext): Promise<TapResult> {
    const ctl = requireController(ctx);

    // Coordinate path: no selector, just (x,y). IME geometric block only.
    if (args.x != null && args.y != null && !args.selector) {
      if (await this.imeGuard.blocks(args.x, args.y, ctl)) {
        return this.blockedCoordInIme();
      }
      await ctl.tapCoordinates(args.x, args.y);
      return { ok: true };
    }

    if (!args.selector) {
      return { ok: false, reason: "tap requires either `selector` or `{x, y}`" };
    }

    // Priority broadening — try resourceId → contentDesc → text → ...
    const { resolved, usedSelector } = await this.resolver.resolve(args.selector, ctl);

    // Fuzzy resourceId fallback (Flutter / Compose / RN non-qualified ids).
    if (!resolved.found && args.selector.resourceId) {
      const summary = await ctl.getUiSummary().catch(() => []);
      const match = this.fuzzy.match(args.selector.resourceId, summary);
      if (match.kind === "single") {
        const el = match.element;
        const cx = Math.round((el.bounds.left + el.bounds.right) / 2);
        const cy = Math.round((el.bounds.top + el.bounds.bottom) / 2);
        await ctl.tapCoordinates(cx, cy);
        return { ok: true, reason: match.reason };
      }
      if (match.kind === "ambiguous") {
        return {
          ok: false,
          reason:
            `Ambiguous resourceId "${args.selector.resourceId}": matches ` +
            `${match.candidates.length} elements. Use the FULL id.`,
          candidates: match.candidates
            .slice(0, 8)
            .map((c) => c.selector?.resourceId)
            .filter(Boolean),
        };
      }
    }

    // Structural IME block on resolved element.
    if (resolved.found && resolved.isInIme === true) {
      return this.blockedElementInIme(usedSelector);
    }

    // Anti-guess: content selector that didn't resolve — stop agent from
    // retrying with another guess.
    if (!resolved.found && this.usedContentSelector(args.selector)) {
      return {
        ok: false,
        reason:
          "NOT FOUND: selector did not match any element. If you guessed the text/label " +
          "without reading the screen, STOP guessing — call `get_ui_tree` or " +
          "`find_element` to see what's actually on screen. App language may differ " +
          "from what you assumed (e.g. 日本語 instead of English).",
        selectorTried: args.selector,
      };
    }

    // Block tapping an editable input by its current text value.
    if (resolved.found && this.isTextTapOnInput(args.selector, resolved)) {
      return {
        ok: false,
        reason:
          "BLOCKED: tapping an input field via text/textContains is wrong — the text " +
          "selector matches the field's CURRENT VALUE, not its identity. Password fields " +
          "hide their value, fresh fields have no value, copy can change. Use " +
          "`input_text({x, y, text})` or `tap_coordinates(x, y)` with the field's center.",
        resolvedBounds: resolved.bounds,
        resolvedRole: resolved.className,
      };
    }

    // Dispatch.
    const warning = checkSelectorQuality(usedSelector, resolved);
    const result = await ctl.tap(usedSelector);
    return warning ? { ...result, selectorWarning: warning } : result;
  }

  private blockedCoordInIme(): TapResult {
    return {
      ok: false,
      reason:
        "BLOCKED: coordinate is inside the IME region. Use input_text instead " +
        "of tapping keyboard keys.",
    };
  }

  private blockedElementInIme(selector: Selector): TapResult {
    return {
      ok: false,
      reason:
        "BLOCKED: element is inside the IME (soft keyboard). Do NOT tap keyboard keys " +
        "individually. Use `input_text({text})` or `input_text({x,y,text})`.",
      blockedSelector: selector,
      isInIme: true,
    };
  }

  private usedContentSelector(s: Selector): boolean {
    return s.text != null || s.textContains != null || s.contentDesc != null;
  }

  private isTextTapOnInput(selector: Selector, resolved: ResolvedElement): boolean {
    const cls = (resolved.className ?? "").toLowerCase();
    const looksLikeInput =
      cls.includes("edittext") ||
      cls.includes("textfield") ||
      cls.includes("textinput") ||
      cls.includes("editable");
    const usedTextSelector =
      (selector.text != null || selector.textContains != null) &&
      !selector.resourceId &&
      !selector.contentDesc;
    return looksLikeInput && usedTextSelector;
  }
}
