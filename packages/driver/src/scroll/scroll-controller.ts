import type { CallOptions, Driver, Size } from "../driver/driver.port.js";
import type { Clock } from "@atomyx/core/infra";
import type { Logger } from "@atomyx/core/infra";
import { NoopLogger } from "@atomyx/core/infra";
import type { ElementFilter } from "../filters/element-filter.js";
import type { TreeCursor } from "../tree/tree-cursor.js";
import { AttrKeys, getAttr } from "../tree/tree-node.js";
import { parseBounds, boundsCenter, type Bounds } from "../tree/bounds.js";
import { Finder } from "../finder/finder.js";

/**
 * Cross-platform scroll-into-view controller.
 *
 * Operates only on the canonical `TreeNode` shape plus the
 * primitive `Driver.swipe()` gesture, so the same two-stage
 * algorithm drives every platform adapter.
 *
 *   Stage 1 — scroll-search (virtualized list recovery)
 *   ─────────────────────────────────────────────────
 *   When the initial `Finder.find()` returns zero matches, we
 *   assume the target may be recycled off-screen by a virtualized
 *   list. Swipe the screen's center column UP repeatedly
 *   (revealing items logically above — the common case after a
 *   Back navigation leaves the list scrolled to the bottom),
 *   polling the filter after each swipe. If still empty after the
 *   UP budget, reverse direction and swipe DOWN for another
 *   budget.
 *
 *   Stage 2 — positional scroll-into-view
 *   ─────────────────────────────────────
 *   Once the target is present in the tree, check whether its
 *   midpoint sits in the safe-area-inset viewport. If yes, done.
 *   If no, compute the direction (element above viewport midY
 *   → scroll content down; element below → scroll content up),
 *   execute one swipe sized to at most `maxScrollFraction *
 *   screenHeight`, re-resolve, loop until visible or bounds stop
 *   moving (edge-progress check).
 *
 * Safe-area insets default to 60pt top (status bar + nav bar
 * large-title zone) and 50pt bottom (home indicator + tab bar).
 * Overridable per instance via the `viewportInsets` option for
 * apps with unusual chrome.
 */
export interface ScrollControllerDeps {
  readonly driver: Pick<Driver, "hierarchy" | "swipe" | "screenSize">;
  readonly clock: Clock;
  readonly logger?: Logger;
}

export interface ScrollControllerOptions {
  /**
   * Maximum positional-scroll iterations after the target is
   * found. Prevents runaway loops on misconfigured scrollables.
   */
  readonly maxIterations?: number;
  /**
   * How far each swipe travels, as a fraction of the screen
   * height. 0.6 = 60% of the screen per swipe, comfortable
   * without overshooting on short lists.
   */
  readonly maxScrollFraction?: number;
  /** Swipe animation duration in milliseconds. */
  readonly swipeDurationMs?: number;
  /** Wait after each swipe for the scroll view to settle. */
  readonly settleWaitMs?: number;
  /**
   * Scroll-search budget — swipes in each direction (UP then
   * DOWN) when the target isn't in the current snapshot at all.
   * Total swipes = 2 × scrollSearchBudget.
   */
  readonly scrollSearchBudget?: number;
  /** Safe-area insets (points) around the "visible" region. */
  readonly viewportInsets?: ViewportInsets;
}

export interface ViewportInsets {
  readonly top: number;
  readonly bottom: number;
  readonly left: number;
  readonly right: number;
}

export const DEFAULT_INSETS: ViewportInsets = {
  top: 60,
  bottom: 50,
  left: 0,
  right: 0,
};

export class ScrollUnreachableError extends Error {
  constructor(
    message: string,
    public readonly finalBounds: Bounds | null,
  ) {
    super(message);
    this.name = "ScrollUnreachableError";
  }
}

export class ScrollController {
  private readonly logger: Logger;
  private readonly finder: Finder;
  private readonly maxIterations: number;
  private readonly maxScrollFraction: number;
  private readonly swipeDurationMs: number;
  private readonly settleWaitMs: number;
  private readonly scrollSearchBudget: number;
  private readonly insets: ViewportInsets;

  constructor(
    private readonly deps: ScrollControllerDeps,
    opts: ScrollControllerOptions = {},
  ) {
    this.logger = deps.logger ?? new NoopLogger();
    this.finder = new Finder({
      driver: deps.driver,
      clock: deps.clock,
      logger: this.logger,
    });
    this.maxIterations = opts.maxIterations ?? 8;
    this.maxScrollFraction = opts.maxScrollFraction ?? 0.6;
    this.swipeDurationMs = opts.swipeDurationMs ?? 200;
    this.settleWaitMs = opts.settleWaitMs ?? 500;
    this.scrollSearchBudget = opts.scrollSearchBudget ?? 6;
    this.insets = opts.viewportInsets ?? DEFAULT_INSETS;
  }

  /**
   * Bring an element matching `filter` into the interactive
   * viewport. Returns the cursor once the element's midpoint
   * sits inside the safe-area-inset rect.
   *
   * Throws `ScrollUnreachableError` if the element cannot be
   * found via scroll-search OR cannot be centered within the
   * iteration budget.
   */
  async ensureVisible(filter: ElementFilter, opts?: CallOptions): Promise<TreeCursor> {
    const screen = await this.deps.driver.screenSize(opts);

    // Stage 1: scroll-search when the initial resolve is empty.
    let match: TreeCursor;
    const initial = (await this.finder.find(filter, opts))[0];
    if (initial) {
      match = initial;
    } else {
      const found = await this.scrollSearch(filter, screen, opts);
      if (!found) {
        throw new ScrollUnreachableError(
          "element not found after scroll-search (UP + DOWN budgets exhausted)",
          null,
        );
      }
      match = found;
    }

    // Stage 2: positional scroll-into-view loop.
    let previousBounds: Bounds | null = null;
    for (let iteration = 0; iteration < this.maxIterations; iteration++) {
      const bounds = this.boundsOf(match);
      if (!bounds) {
        // Element has no geometry — can't position-scroll. Accept
        // and let the caller try a tap with the center they
        // compute from whatever data they have.
        return match;
      }
      if (this.isInsideInsetViewport(bounds, screen)) {
        return match;
      }

      if (previousBounds && boundsEqual(previousBounds, bounds)) {
        throw new ScrollUnreachableError(
          `element at ${formatB(bounds)} did not move after scroll ` +
            `iteration ${iteration}. Likely at scroll-region edge.`,
          bounds,
        );
      }
      previousBounds = bounds;

      await this.scrollTowardElement(bounds, screen, opts);
      await this.deps.clock.sleep(this.settleWaitMs);

      const next = (await this.finder.find(filter, opts))[0];
      if (!next) {
        throw new ScrollUnreachableError(
          `element lost after scroll attempt ${iteration + 1} — ` +
            `filter no longer matches after container reload`,
          bounds,
        );
      }
      match = next;
    }

    throw new ScrollUnreachableError(
      `element remained off-screen after ${this.maxIterations} iterations`,
      this.boundsOf(match),
    );
  }

  /**
   * Alternating-direction probe. Swipes UP for budget iterations,
   * then DOWN for budget iterations, polling the filter between
   * each swipe. Returns the first cursor that matches, or
   * `undefined` if both budgets are exhausted.
   */
  private async scrollSearch(
    filter: ElementFilter,
    screen: Size,
    opts?: CallOptions,
  ): Promise<TreeCursor | undefined> {
    for (let i = 0; i < this.scrollSearchBudget; i++) {
      await this.swipeCenter(screen, "up", opts);
      await this.deps.clock.sleep(this.settleWaitMs);
      const found = (await this.finder.find(filter, opts))[0];
      if (found) return found;
    }
    for (let i = 0; i < this.scrollSearchBudget; i++) {
      await this.swipeCenter(screen, "down", opts);
      await this.deps.clock.sleep(this.settleWaitMs);
      const found = (await this.finder.find(filter, opts))[0];
      if (found) return found;
    }
    return undefined;
  }

  /**
   * Execute one scroll gesture sized + directed to move the
   * element toward the viewport center. Scroll amount is clamped
   * to `maxScrollFraction * screenHeight` so each iteration makes
   * a predictable step.
   */
  private async scrollTowardElement(
    bounds: Bounds,
    screen: Size,
    opts?: CallOptions,
  ): Promise<void> {
    const elemMidX = (bounds.left + bounds.right) / 2;
    const elemMidY = (bounds.top + bounds.bottom) / 2;
    const viewportMidY = screen.height / 2;

    const swipeX = clamp(elemMidX, 40, screen.width - 40);
    const deltaY = elemMidY - viewportMidY;
    const scrollAmount = Math.min(
      Math.abs(deltaY),
      screen.height * this.maxScrollFraction,
    );
    // Element below viewport → finger drags bottom-to-top (content moves up).
    // Element above viewport → finger drags top-to-bottom (content moves down).
    const elementBelow = deltaY > 0;
    const fromY = elementBelow
      ? viewportMidY + scrollAmount / 2
      : viewportMidY - scrollAmount / 2;
    const toY = elementBelow
      ? viewportMidY - scrollAmount / 2
      : viewportMidY + scrollAmount / 2;

    await this.deps.driver.swipe(
      { x: swipeX, y: fromY },
      { x: swipeX, y: toY },
      this.swipeDurationMs,
      opts,
    );
  }

  /**
   * Blind center-column swipe used by `scrollSearch`. Distance is
   * fixed to `maxScrollFraction` — not adaptive, because we don't
   * know where the target is yet.
   */
  private async swipeCenter(
    screen: Size,
    direction: "up" | "down",
    opts?: CallOptions,
  ): Promise<void> {
    const x = screen.width / 2;
    const midY = screen.height / 2;
    const delta = (screen.height * this.maxScrollFraction) / 2;
    // "up" = reveal items logically ABOVE: finger drags down, content scrolls down.
    // "down" = reveal items BELOW: finger drags up, content scrolls up.
    const fromY = direction === "up" ? midY - delta : midY + delta;
    const toY = direction === "up" ? midY + delta : midY - delta;
    await this.deps.driver.swipe(
      { x, y: fromY },
      { x, y: toY },
      this.swipeDurationMs,
      opts,
    );
  }

  private boundsOf(cursor: TreeCursor): Bounds | null {
    return parseBounds(getAttr(cursor.node, AttrKeys.Bounds));
  }

  private isInsideInsetViewport(b: Bounds, screen: Size): boolean {
    const { x, y } = boundsCenter(b);
    return (
      x >= this.insets.left &&
      x < screen.width - this.insets.right &&
      y >= this.insets.top &&
      y < screen.height - this.insets.bottom
    );
  }
}

function clamp(v: number, min: number, max: number): number {
  if (max < min) return min;
  return Math.min(Math.max(v, min), max);
}

function boundsEqual(a: Bounds, b: Bounds): boolean {
  return (
    a.left === b.left &&
    a.top === b.top &&
    a.right === b.right &&
    a.bottom === b.bottom
  );
}

function formatB(b: Bounds): string {
  return `[${b.left},${b.top},${b.right},${b.bottom}]`;
}
