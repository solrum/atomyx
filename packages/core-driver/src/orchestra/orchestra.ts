import type { Driver, KeyCode, Point, Size, LaunchArgs } from "../driver/driver.port.js";
import type { Clock } from "../infra/clock.port.js";
import type { Logger } from "../infra/logger.port.js";
import { NoopLogger } from "../infra/logger.port.js";
import type { TreeNode } from "../tree/tree-node.js";
import type { TreeCursor } from "../tree/tree-cursor.js";
import { rootNodeOf } from "../tree/tree-cursor.js";
import { AttrKeys, getAttr } from "../tree/tree-node.js";
import { parseBounds, boundsCenter } from "../tree/bounds.js";
import type { Selector } from "../selectors/selector.js";
import { compileSelector } from "../selectors/priority-broadening.js";
import { Finder, type WaitOptions } from "../finder/finder.js";
import { ScrollController, ScrollUnreachableError } from "../scroll/scroll-controller.js";
import { detectObscurement } from "../obscurement/obscurement.js";
import { type ActionResult, ok as okResult, fail as failResult } from "./action-result.js";

/**
 * The Orchestra is the high-level command layer of Atomyx. It is
 * the ONE place that composes:
 *
 *   Driver (primitives)
 *     + Finder (tree query + wait)
 *       + ScrollController (scroll-into-view)
 *         + compileSelector (priority broadening policy)
 *           + detectObscurement (z-order safety)
 *             = tap(selector) / inputText(selector) / ...
 *
 * Feature consumers (MCP server tools, Studio replay, CLI run
 * mode, Synapse runner) never assemble these pieces themselves —
 * they construct one `Orchestra` per session and call its action
 * methods. This keeps the "right way to use the framework"
 * explicit and prevents feature-side drift on how tap-with-
 * selector should actually behave.
 *
 * Contract:
 *
 *   - Every action that takes a `Selector` runs it through
 *     `compileSelector` (priority broadening: id > label > text
 *     > value > hint; role/enabled/clickable AND-ed).
 *
 *   - Every action that takes a selector goes through
 *     `ScrollController.ensureVisible` first. This covers two
 *     failure modes that the god-class iOS adapter used to ship
 *     inline: virtualized-list recovery (Phase 0 scroll-search)
 *     and positional scroll-into-view with safe-area insets.
 *
 *   - Every action then runs `detectObscurement` on the resolved
 *     node against the current hierarchy. An obscured element is
 *     NOT tapped blindly — the action returns
 *     `{ok:false, reason, obscurer}` with enough information for
 *     the consumer to dismiss the obscurer or tap it directly.
 *
 *   - Typed errors from the inner components
 *     (`ScrollUnreachableError`, `FindTimeoutError`) are caught
 *     at the Orchestra boundary and converted to `ok:false`
 *     ActionResult. Unexpected errors (driver transport failure,
 *     programming errors) propagate as exceptions — those are
 *     infrastructure problems, not action outcomes.
 *
 *   - Coordinate-based actions (`tapAt`, `swipeAt`, `longPressAt`)
 *     bypass selector/scroll/obscurement entirely and go straight
 *     to the driver. Useful as a fallback when an agent can see
 *     "where to tap" from a screenshot but has no stable selector.
 *
 * Dependency injection:
 *
 *   - `driver` is the only required dependency. Finder and
 *     ScrollController are constructed internally with their
 *     narrow Driver subsets. Consumers who need to swap Finder
 *     or ScrollController behavior (custom retry policy, different
 *     scroll insets) should NOT subclass Orchestra — they should
 *     inject configured instances via the `finder` / `scroll`
 *     overrides below.
 *
 *   - `clock` defaults to a `SystemClock` is NOT provided here;
 *     it IS required. Orchestra refuses to construct without an
 *     explicit clock so tests stay deterministic by default and
 *     accidental `Date.now()` reliance in core is caught early.
 *
 *   - `logger` defaults to `NoopLogger`. Provide a real logger in
 *     production flows.
 */
export interface OrchestraDeps {
  readonly driver: Driver;
  readonly clock: Clock;
  readonly logger?: Logger;
  /**
   * Optional pre-configured Finder. Defaults to `new Finder({driver,
   * clock, logger})`. Override when you need a custom poll interval
   * or when several Orchestra instances should share a finder.
   */
  readonly finder?: Finder;
  /**
   * Optional pre-configured ScrollController. Defaults to
   * `new ScrollController({driver, clock, logger})`. Override to
   * supply custom viewport insets, scroll iteration budget, or
   * scroll-search direction preference.
   */
  readonly scroll?: ScrollController;
}

export class Orchestra {
  private readonly driver: Driver;
  private readonly clock: Clock;
  private readonly logger: Logger;
  private readonly finder: Finder;
  private readonly scroll: ScrollController;

  constructor(deps: OrchestraDeps) {
    this.driver = deps.driver;
    this.clock = deps.clock;
    this.logger = deps.logger ?? new NoopLogger();
    this.finder =
      deps.finder ?? new Finder({ driver: deps.driver, clock: deps.clock, logger: this.logger });
    this.scroll =
      deps.scroll ??
      new ScrollController({ driver: deps.driver, clock: deps.clock, logger: this.logger });
  }

  // ── Tree inspection ──────────────────────────────────────────

  /** Current UI hierarchy as a canonical `TreeNode`. */
  async hierarchy(): Promise<TreeNode> {
    return this.driver.hierarchy();
  }

  /** Single-shot find. Returns all matches of the compiled selector. */
  async find(selector: Selector): Promise<TreeCursor[]> {
    return this.finder.find(compileSelector(selector));
  }

  /** Single-shot find, first match or null. */
  async findOne(selector: Selector): Promise<TreeCursor | null> {
    return this.finder.findOne(compileSelector(selector));
  }

  /**
   * Polling wait for selector. Throws `FindTimeoutError` on
   * deadline miss — this is a "caller wanted a specific element
   * and didn't get it" scenario, not an ActionResult case.
   */
  async waitFor(selector: Selector, opts: WaitOptions): Promise<TreeCursor[]> {
    return this.finder.waitFor(compileSelector(selector), opts);
  }

  // ── Selector actions ─────────────────────────────────────────

  /**
   * Tap the element matching `selector`. Full pipeline:
   * compile → scroll-into-view → obscurement check → coordinate
   * tap. Returns `ActionResult` — never throws on action-level
   * failure, only on infrastructure errors.
   */
  async tap(selector: Selector): Promise<ActionResult> {
    const prepared = await this.prepareSelectorForAction(selector);
    if (!prepared.ok) return prepared.result;
    await this.driver.tap(prepared.point);
    return okResult({ resolvedBy: prepared.resolvedBy, detail: `tapped at ${pointStr(prepared.point)}` });
  }

  /**
   * Long-press the element matching `selector`. Same pipeline as
   * `tap`, but dispatches a long-press primitive instead of tap.
   */
  async longPress(selector: Selector, durationMs = 500): Promise<ActionResult> {
    const prepared = await this.prepareSelectorForAction(selector);
    if (!prepared.ok) return prepared.result;
    await this.driver.longPress(prepared.point, durationMs);
    return okResult({
      resolvedBy: prepared.resolvedBy,
      detail: `long-pressed at ${pointStr(prepared.point)} for ${durationMs}ms`,
    });
  }

  /**
   * Type `text` into the element matching `selector`. Pipeline:
   * compile → scroll-into-view → obscurement → tap-to-focus →
   * inputText primitive. iOS and Android drivers both require a
   * focus tap before `inputText` works, so we do it here instead
   * of pushing the concern into every feature.
   *
   * `opts.clearFirst` (default true) erases existing content with
   * `driver.eraseText(999)` before typing. Set false when
   * appending to an existing value.
   */
  async inputText(
    selector: Selector,
    text: string,
    opts: { clearFirst?: boolean } = {},
  ): Promise<ActionResult> {
    const prepared = await this.prepareSelectorForAction(selector);
    if (!prepared.ok) return prepared.result;
    await this.driver.tap(prepared.point);
    if (opts.clearFirst !== false && this.driver.capabilities.canEraseText) {
      await this.driver.eraseText(999);
    }
    await this.driver.inputText(text);
    return okResult({
      resolvedBy: prepared.resolvedBy,
      detail: `typed ${text.length} char(s) into field at ${pointStr(prepared.point)}`,
    });
  }

  // ── Coordinate primitives (bypass selector pipeline) ────────

  async tapAt(point: Point): Promise<void> {
    await this.driver.tap(point);
  }

  async longPressAt(point: Point, durationMs = 500): Promise<void> {
    await this.driver.longPress(point, durationMs);
  }

  async swipeAt(from: Point, to: Point, durationMs = 200): Promise<void> {
    await this.driver.swipe(from, to, durationMs);
  }

  /**
   * Directional swipe centered on the screen. Convenience over
   * `swipeAt` — resolves screen dimensions from the driver and
   * computes endpoints. Swipe distance is 60% of the screen in
   * the swipe axis, chosen to match `ScrollController`'s
   * `maxScrollFraction` default.
   */
  async swipeDirection(
    direction: "up" | "down" | "left" | "right",
    opts: { durationMs?: number; fraction?: number } = {},
  ): Promise<void> {
    const screen: Size = await this.driver.screenSize();
    const fraction = opts.fraction ?? 0.6;
    const durationMs = opts.durationMs ?? 200;
    const cx = screen.width / 2;
    const cy = screen.height / 2;
    const dx = direction === "left" ? -1 : direction === "right" ? 1 : 0;
    const dy = direction === "up" ? -1 : direction === "down" ? 1 : 0;
    const amountX = (screen.width * fraction) / 2;
    const amountY = (screen.height * fraction) / 2;
    const from: Point = {
      x: cx - (dx * amountX) / 2,
      y: cy - (dy * amountY) / 2,
    };
    const to: Point = {
      x: cx + (dx * amountX) / 2,
      y: cy + (dy * amountY) / 2,
    };
    await this.driver.swipe(from, to, durationMs);
  }

  // ── Keyboard + input primitives ──────────────────────────────

  async pressKey(key: KeyCode): Promise<ActionResult> {
    const result = await this.driver.pressKey(key);
    return result.ok
      ? okResult({ detail: result.reason })
      : failResult(result.reason ?? `pressKey(${key}) returned ok:false`);
  }

  async typeText(text: string): Promise<void> {
    await this.driver.inputText(text);
  }

  async eraseText(count: number): Promise<void> {
    if (!this.driver.capabilities.canEraseText) {
      throw new Error("driver does not support eraseText — use pressKey('delete') N times");
    }
    await this.driver.eraseText(count);
  }

  // ── App lifecycle ────────────────────────────────────────────

  async launchApp(appId: string, args?: LaunchArgs): Promise<void> {
    await this.driver.launchApp(appId, args);
  }

  async stopApp(appId: string): Promise<void> {
    await this.driver.stopApp(appId);
  }

  async killApp(appId: string): Promise<void> {
    await this.driver.killApp(appId);
  }

  /** Enumerate installed apps on the current device. */
  async listApps(): Promise<readonly import("../driver/driver.port.js").InstalledApp[]> {
    return this.driver.listApps();
  }

  /** Foreground app info (bundleId + optional activity). */
  async currentForeground(): Promise<import("../driver/driver.port.js").ForegroundInfo> {
    return this.driver.currentForeground();
  }

  /** Device info (platform, version, model, udid, kind). */
  async deviceInfo(): Promise<import("../driver/driver.port.js").DeviceInfo> {
    return this.driver.deviceInfo();
  }

  // ── Media ────────────────────────────────────────────────────

  async screenshot(): Promise<Uint8Array> {
    return this.driver.screenshot();
  }

  // ── Idle detection ───────────────────────────────────────────

  /**
   * Block until the UI reaches an idle state or the deadline
   * expires. Uses the driver's native `waitForIdle` when
   * available; otherwise falls back to a host-side tree-diff
   * polling loop driven by the Clock.
   */
  async waitForIdle(timeoutMs: number): Promise<boolean> {
    if (this.driver.capabilities.canWaitForIdle) {
      return this.driver.waitForIdle(timeoutMs);
    }
    return this.waitForIdleHostSide(timeoutMs);
  }

  private async waitForIdleHostSide(timeoutMs: number): Promise<boolean> {
    const pollMs = 200;
    const deadline = this.clock.now() + timeoutMs;
    let previous = JSON.stringify(await this.driver.hierarchy());
    while (this.clock.now() < deadline) {
      await this.clock.sleep(pollMs);
      const current = JSON.stringify(await this.driver.hierarchy());
      if (current === previous) return true;
      previous = current;
    }
    return false;
  }

  // ── Internal: selector preparation pipeline ──────────────────

  /**
   * Core pipeline that every selector-based action shares.
   * Returns either a prepared coordinate + metadata (on success)
   * or a ready-to-return ActionResult (on failure). Splitting
   * this out of the individual action methods keeps tap /
   * longPress / inputText logic trivial.
   */
  private async prepareSelectorForAction(
    selector: Selector,
  ): Promise<
    | { ok: true; point: Point; cursor: TreeCursor; resolvedBy: string | undefined }
    | { ok: false; result: ActionResult }
  > {
    const filter = compileSelector(selector);

    // Scroll into view (includes scroll-search for virtualized lists
    // and positional inset-aware centering).
    let cursor: TreeCursor;
    try {
      cursor = await this.scroll.ensureVisible(filter);
    } catch (err) {
      if (err instanceof ScrollUnreachableError) {
        return {
          ok: false,
          result: failResult(
            `could not scroll element into view: ${err.message}`,
          ),
        };
      }
      throw err;
    }

    // Obscurement check against the SAME tree the cursor points
    // into. We walk the cursor's parent chain up to the root and
    // pass that `TreeNode` to `detectObscurement` — NOT a fresh
    // `driver.hierarchy()` call.
    //
    // Why not fetch a fresh hierarchy here: `detectObscurement`
    // uses reference identity (`topmost === target` +
    // `containsNode(reference walk)`) for the ancestor
    // disambiguation path. On real drivers that rebuild the
    // TreeNode graph from JSON per call, two successive
    // `hierarchy()` calls produce referentially-distinct node
    // instances even for identical screen state. Passing a fresh
    // root + the cursor's stale node would make every reference
    // check fail, collapsing the ancestor path into dead code and
    // falling through to the generic-container suppression
    // (which only covers role=container/other with empty
    // id/label). Any interior leaf would then get flagged
    // "obscured by itself".
    //
    // By reusing the cursor's own tree we keep reference identity
    // valid, detect real obscurers correctly, AND save one
    // hierarchy RPC per selector action.
    const tree = rootNodeOf(cursor);
    const ob = detectObscurement(tree, cursor.node);
    if (ob.obscured) {
      return {
        ok: false,
        result: failResult(
          `element is visually obscured by [role=${ob.obscurer.role} id="${ob.obscurer.id}" label="${ob.obscurer.label}"]. ` +
            `Dismiss the obscuring element, or call tap on the obscurer directly.`,
          { obscurer: ob.obscurer },
        ),
      };
    }

    // Compute tap center from bounds.
    const bounds = parseBounds(getAttr(cursor.node, AttrKeys.Bounds));
    if (!bounds) {
      return {
        ok: false,
        result: failResult(
          "element resolved but has no bounds attribute; cannot compute tap coordinate",
        ),
      };
    }
    const point = boundsCenter(bounds);

    // Determine which priority field actually resolved the match.
    // Priority broadening doesn't report this today — we reverse-
    // engineer it by matching the cursor's attributes against the
    // selector's provided fields in the same order compileSelector
    // uses. Not perfect (a selector matching `id` AND `text` would
    // report `id` even if we internally tried text), but matches
    // the broadening policy.
    const resolvedBy = this.guessResolvedBy(selector, cursor.node);

    this.logger.debug("selector prepared", {
      resolvedBy,
      point,
      bounds,
    });

    return { ok: true, point, cursor, resolvedBy };
  }

  private guessResolvedBy(selector: Selector, node: TreeNode): string | undefined {
    const attrs = node.attributes;
    if (selector.id !== undefined && matches(attrs[AttrKeys.Id], selector.id)) return "id";
    if (selector.label !== undefined && matches(attrs[AttrKeys.Label], selector.label)) return "label";
    if (selector.text !== undefined && matches(attrs[AttrKeys.Text], selector.text)) return "text";
    if (selector.value !== undefined && matches(attrs[AttrKeys.Value], selector.value)) return "value";
    if (selector.hint !== undefined && matches(attrs[AttrKeys.Hint], selector.hint)) return "hint";
    return undefined;
  }
}

function matches(value: string | undefined, pattern: string | RegExp): boolean {
  if (value === undefined) return false;
  if (typeof pattern === "string") return value === pattern;
  return pattern.test(value);
}

function pointStr(p: Point): string {
  return `(${Math.round(p.x)},${Math.round(p.y)})`;
}
