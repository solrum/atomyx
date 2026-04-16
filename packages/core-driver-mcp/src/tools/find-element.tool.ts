import { defineTool, orchestraOrFail } from "../tool-definition.js";
import { SelectorSchema, compileSelectorInput } from "../selector-schema.js";
import { AttrKeys, parseBounds, boundsCenter } from "@atomyx/core-driver";

const FindElementArgs = SelectorSchema;

/**
 * `find_element` — selector → element details. Returns enough
 * info for the agent to either tap the element directly via
 * `tap` or fall back to coordinates from the returned center.
 *
 * Uses Orchestra's `findOne` which runs the priority broadening
 * pipeline. Does NOT scroll-into-view — for "make it visible
 * AND tap", use `tap` directly which composes both steps.
 */
export const findElementTool = defineTool({
  name: "find_element",
  description:
    "Find a single element matching a selector. Returns the element's id, " +
    "text, label, role, bounds, and tap center point. Use this to verify " +
    "an element exists or to get coordinates when a selector-based tap " +
    "is failing for some reason. Selector priority: id > label > text > " +
    "value > hint, with role/enabled/clickable/focused as constraints.",
  inputSchema: FindElementArgs,
  async execute(args, ctx) {
    const orchestra = orchestraOrFail(ctx);
    const selector = compileSelectorInput(args);
    const cursor = await orchestra.findOne(selector);
    if (!cursor) {
      return { found: false };
    }
    const attrs = cursor.node.attributes;
    const bounds = parseBounds(attrs[AttrKeys.Bounds]);
    return {
      found: true,
      id: attrs[AttrKeys.Id],
      text: attrs[AttrKeys.Text],
      label: attrs[AttrKeys.Label],
      value: attrs[AttrKeys.Value],
      role: attrs[AttrKeys.Role],
      bounds,
      center: bounds ? boundsCenter(bounds) : null,
      enabled: cursor.node.enabled,
      clickable: cursor.node.clickable,
    };
  },
});
