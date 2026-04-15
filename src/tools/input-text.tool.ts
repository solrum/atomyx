import type {
  Selector,
  TypeKeyboardResult,
} from "../adapters/device-controller.port.js";
import { requireController, type AdetContext } from "../runtime/adet-context.js";
import type { JsonSchema } from "../types.js";
import { StructuralInputFinder } from "./core/index.js";
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

export interface InputTextArgs {
  selector?: Selector;
  x?: number;
  y?: number;
  text: string;
  clearFirst?: boolean;
}

export interface InputTextResult {
  ok: boolean;
  typed?: number;
  total?: number;
  reason?: string;
  strategy?: string;
  selectorWarning?: string;
  selectorTried?: Selector;
}

/**
 * Type text into an input field. One tool, three paths:
 *
 *   1. `{x, y, text}` — direct coordinates, no resolver roundtrip.
 *   2. `{selector, text}` where selector points at a real EditText —
 *      fast-path ACTION_SET_TEXT.
 *   3. `{selector, text}` where selector points at a label / container /
 *      wrapper — runs `StructuralInputFinder` to locate the real EditText,
 *      then tap-focus + type at its center.
 *
 * Consolidates input_text + fill_input_at_coordinates + type_via_keyboard +
 * clear_focused_input. Agent doesn't need to know platform convention —
 * the right path is picked based on what resolves.
 */
export class InputTextTool extends Tool<{
  args: InputTextArgs;
  result: InputTextResult;
}> {
  readonly name = "input_text";
  readonly description =
    "Type text into an input field. Accepts `{selector, text}` OR `{x, y, text}`. " +
    "If the selector points at a label/container rather than the EditText itself, runs a " +
    "structural strategy chain to find the real field. Focus + clear + type in one call. " +
    "Works with native IMEs and custom in-app keypads.";
  readonly schema: JsonSchema = {
    type: "object",
    required: ["text"],
    properties: {
      selector: selectorSchema,
      x: { type: "number" },
      y: { type: "number" },
      text: { type: "string" },
      clearFirst: { type: "boolean", default: true },
    },
  };

  constructor(private readonly inputFinder: StructuralInputFinder) {
    super();
  }

  async execute(args: InputTextArgs, ctx: AdetContext): Promise<InputTextResult> {
    const ctl = requireController(ctx);
    const clearFirst = args.clearFirst ?? true;

    // Path 1: explicit coordinates.
    if (args.x != null && args.y != null && !args.selector) {
      return this.fillAt(ctl, args.x, args.y, args.text, clearFirst);
    }

    if (!args.selector) {
      return { ok: false, reason: "input_text requires `selector` or `{x, y}`" };
    }

    const resolved = await ctl.resolveSelector(args.selector);
    const warning = checkSelectorQuality(args.selector, resolved);

    // Path 2: resolved element is a real EditText with stable id → ACTION_SET_TEXT.
    if (resolved.found && this.isEditable(resolved.className) && this.hasStableId(resolved)) {
      const r = await ctl.inputText(args.selector, args.text);
      return this.withWarning(
        { ok: r.ok, reason: r.reason } as InputTextResult,
        warning,
      );
    }

    // Path 3: structural fallback — selector points at label/container.
    const match = await this.inputFinder.find(
      {
        resourceId: args.selector.resourceId,
        contentDesc: args.selector.contentDesc,
        label: args.selector.text,
        keyword:
          args.selector.textContains ??
          args.selector.hint ??
          (args.selector.text && args.selector.text.length <= 12
            ? args.selector.text
            : undefined),
      },
      ctl,
    );
    if (!match || !match.element.bounds) {
      return this.withWarning(
        {
          ok: false,
          reason:
            "input_text could not locate an editable field. Selector did not match a " +
            "label/container near the target field. Call get_ui_tree to inspect available labels.",
          selectorTried: args.selector,
        },
        warning,
      );
    }
    const b = match.element.bounds;
    const cx = Math.round((b.left + b.right) / 2);
    const cy = Math.round((b.top + b.bottom) / 2);
    const filled = await this.fillAt(ctl, cx, cy, args.text, clearFirst);
    return this.withWarning({ ...filled, strategy: match.strategy }, warning);
  }

  private async fillAt(
    ctl: ReturnType<typeof requireController>,
    x: number,
    y: number,
    text: string,
    clearFirst: boolean,
  ): Promise<InputTextResult> {
    await ctl.tapCoordinates(x, y);
    await new Promise((r) => setTimeout(r, 120));
    const r: TypeKeyboardResult = await ctl.typeViaKeyboard(text, 50, clearFirst);
    return { ok: r.success, typed: r.typed, total: r.total, reason: r.reason };
  }

  private isEditable(className: string | null | undefined): boolean {
    const cls = (className ?? "").toLowerCase();
    return (
      cls.includes("edittext") ||
      cls.includes("textfield") ||
      cls.includes("textinput") ||
      cls.includes("editable")
    );
  }

  private hasStableId(resolved: { resourceId?: string | null; contentDesc?: string | null; text?: string | null }): boolean {
    return Boolean(resolved.resourceId || resolved.contentDesc || resolved.text);
  }

  private withWarning(result: InputTextResult, warning: string | undefined): InputTextResult {
    return warning ? { ...result, selectorWarning: warning } : result;
  }
}
