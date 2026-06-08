import { getFeature } from "../../../state/core/registry.js";
import type { ActionsApi } from "../../../state/features/actions/index.js";
import { ACTIONS_KEY } from "../../../state/features/actions/index.js";
import type { KeyMatcher } from "../../../domain/features/actions/index.js";

let installed = false;

function keyMatches(event: KeyboardEvent, matcher: KeyMatcher): boolean {
  if (event.key.toLowerCase() !== matcher.key.toLowerCase()) return false;
  if ((matcher.meta ?? false) !== event.metaKey) return false;
  if ((matcher.ctrl ?? false) !== event.ctrlKey) return false;
  if ((matcher.shift ?? false) !== event.shiftKey) return false;
  if ((matcher.alt ?? false) !== event.altKey) return false;
  return true;
}

/**
 * Install the global keymap dispatcher. Listens on `window` for
 * every keydown, matches against `ACTION_DEFINITIONS[].keyMatcher`,
 * and dispatches the first match.
 *
 * Idempotent: safe to call on every mount.
 */
export function installKeymap(): void {
  if (installed) return;
  installed = true;

  window.addEventListener(
    "keydown",
    (event) => {
      const api = getFeature<ActionsApi>(ACTIONS_KEY);
      const { definitions } = api.getSnapshot();
      for (const def of definitions) {
        if (!def.keyMatcher) continue;
        if (keyMatches(event, def.keyMatcher)) {
          event.preventDefault();
          void api.execute(def.id);
          return;
        }
      }
    },
    { capture: true },
  );
}
