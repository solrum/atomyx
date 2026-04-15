import type { ResolvedElement, Selector } from "../adapters/device-controller.port.js";

/**
 * Inspect a selector + its resolved element and warn if the caller picked a
 * fragile field (text / textContains / hint) when the resolved element actually
 * exposes a stable id (resourceId or contentDesc).
 *
 * Returned string is meant to be attached to the tool result under
 * `selectorWarning` so the agent sees the correction inline without breaking
 * the call.
 */
export function checkSelectorQuality(
  selector: Selector,
  resolved: ResolvedElement | undefined | null,
): string | undefined {
  if (!resolved || !resolved.found) return undefined;

  const usedFragile =
    selector.text != null ||
    selector.textContains != null ||
    selector.hint != null;
  if (!usedFragile) return undefined;

  const usedStable = selector.resourceId != null || selector.contentDesc != null;
  if (usedStable) return undefined;

  const hasResourceId = !!resolved.resourceId;
  const hasContentDesc = !!resolved.contentDesc;
  if (!hasResourceId && !hasContentDesc) return undefined;

  const preferred = hasResourceId
    ? `resourceId="${resolved.resourceId}"`
    : `contentDesc="${resolved.contentDesc}"`;
  const used = selector.text != null
    ? `text="${selector.text}"`
    : selector.textContains != null
    ? `textContains="${selector.textContains}"`
    : `hint="${selector.hint}"`;
  return (
    `FRAGILE SELECTOR: you used ${used} but the resolved element has a stable id — ` +
    `switch to ${preferred}. Text-based selectors break on i18n and copy changes.`
  );
}
