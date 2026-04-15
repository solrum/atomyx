import type { DeviceController, RawElement } from "../../adapters/device-controller.port.js";
import {
  collectInputs,
  findInput,
  type InputMatch,
  type InputQuery,
} from "../find-input.js";

export type { InputMatch, InputQuery };

/**
 * Locates the real editable text field for a semantic query, regardless of
 * how the framework wraps it. Encapsulates the 4-strategy chain (see
 * `find-input.ts`):
 *
 *   1. following_sibling_edittext          (Pattern B — label + field row)
 *   2. following_sibling_container_edittext (Pattern A — icon-wrapped field)
 *   3. descendant_edittext                  (anchor wraps input)
 *   4. self_is_edittext                     (rare, explicit Semantics)
 *
 * This class is the injection point — the underlying pure functions in
 * `find-input.ts` remain free for unit testing without a device.
 */
export class StructuralInputFinder {
  async find(
    query: InputQuery,
    controller: Pick<DeviceController, "getUiTree">,
  ): Promise<InputMatch | null> {
    const raw = await controller.getUiTree();
    return findInput(raw, query);
  }

  /**
   * Enumerate every input field with its semantic label, sorted top-to-bottom.
   * Used by `launch_app` to pre-populate `inputs[]` so the agent has every
   * form field addressable without scanning the tree manually.
   */
  async collectAll(
    controller: Pick<DeviceController, "getUiTree">,
  ): Promise<ReturnType<typeof collectInputs>> {
    const raw = await controller.getUiTree();
    return collectInputs(raw);
  }

  findInRaw(raw: RawElement, query: InputQuery): InputMatch | null {
    return findInput(raw, query);
  }
}
