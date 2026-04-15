import type { DeviceController, ResolvedElement, Selector } from "../../adapters/device-controller.port.js";

/**
 * Resolves a Selector to a device node by trying selector types in a strict
 * priority order — regardless of which type the agent passed. The agent
 * doesn't need to know platform conventions (e.g. that Android exposes most
 * content via `contentDesc`, not `text`); the pipeline tries them all with
 * contentDesc prioritized on Android.
 *
 * Priority (highest to lowest):
 *   1. resourceId    — most stable, locale-independent
 *   2. contentDesc   — Android primary content selector (Material / Compose)
 *   3. text          — visible text, often empty on buttons/icons
 *   4. textContains  — substring fallback
 *   5. hint          — fuzzy last resort
 *
 * The caller ends up with:
 *   - the original `Selector` they passed (if it resolved as-is)
 *   - OR a broadened variant that succeeded
 *   - OR `found: false`
 */
export class SelectorResolutionPipeline {
  async resolve(
    selector: Selector,
    controller: Pick<DeviceController, "resolveSelector">,
  ): Promise<{ resolved: ResolvedElement; usedSelector: Selector }> {
    const attempts = this.buildAttemptList(selector);

    // Dedupe identical attempts.
    const seen = new Set<string>();
    const unique = attempts.filter((a) => {
      const k = JSON.stringify(a);
      if (seen.has(k)) return false;
      seen.add(k);
      return true;
    });

    for (const attempt of unique) {
      const r = await controller.resolveSelector(attempt);
      if (r.found) return { resolved: r, usedSelector: attempt };
    }

    return { resolved: { found: false }, usedSelector: selector };
  }

  /**
   * Build the priority-ordered attempt list from whatever fields the agent
   * provided. `contentValue` is the one string the agent meant for "match
   * this content" — whether they filed it under contentDesc, text, or
   * textContains. We try it under the higher-priority selector types first.
   *
   * `nth` is orthogonal to query type — it's propagated to every attempt.
   */
  private buildAttemptList(selector: Selector): Selector[] {
    const contentValue = selector.contentDesc ?? selector.text ?? selector.textContains;
    const attempts: Selector[] = [];
    const withNth = (s: Selector): Selector =>
      selector.nth != null ? { ...s, nth: selector.nth } : s;

    if (selector.resourceId) attempts.push(withNth({ resourceId: selector.resourceId }));
    if (contentValue) {
      attempts.push(withNth({ contentDesc: contentValue }));
      attempts.push(withNth({ text: contentValue }));
      attempts.push(withNth({ textContains: contentValue }));
    }
    if (selector.hint) attempts.push(withNth({ hint: selector.hint }));
    else if (contentValue) attempts.push(withNth({ hint: contentValue }));

    return attempts;
  }
}
