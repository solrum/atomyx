import type {
  BuiltYaml,
  ScriptAction,
  SelectorCandidate,
  UiTreeNode,
} from "./script-actions.types.js";

function yamlQuote(value: string): string {
  return `"${value.replace(/\\/g, "\\\\").replace(/"/g, '\\"')}"`;
}

function plain(yaml: string): BuiltYaml {
  return { yaml, placeholders: [] };
}

function buildSelectorStep(
  command: string,
  selector: SelectorCandidate,
): BuiltYaml {
  return plain(`- ${command}: ${selector.toYamlInline()}`);
}

/**
 * Heuristic: a node is a text input when its semantic role says so,
 * when its native class carries a known input-type substring, or
 * when it exposes a `hint` (placeholder text is near-unique to
 * fields). Buttons and containers return false so `type` stays out
 * of their menu.
 */
function isTextInput(node: UiTreeNode): boolean {
  const role = (node.attributes["role"] ?? "").toLowerCase();
  if (role === "text-field" || role === "textbox" || role === "searchbox") {
    return true;
  }
  const cls = (node.attributes["class"] ?? "").toLowerCase();
  if (
    cls.includes("edittext") ||
    cls.includes("textfield") ||
    cls.includes("textview") ||
    cls.includes("textbox") ||
    cls.endsWith("input")
  ) {
    return true;
  }
  if (node.attributes["hint"]) return true;
  return false;
}

const TAP: ScriptAction = {
  id: "tap",
  label: "Tap",
  requiresSelector: true,
  appliesTo: () => true,
  buildYaml: (_node, s) => buildSelectorStep("tap", s),
};

const TYPE: ScriptAction = {
  id: "type",
  label: "Type into field",
  requiresSelector: true,
  appliesTo: isTextInput,
  buildYaml: (node, s) => {
    const existing = node.attributes["text"];
    const text = existing && existing.length > 0 ? existing : "TODO";
    const quoted = yamlQuote(text);

    // Multi-field selectors (role + nth) render as block form so
    // the generated step reads as "- type: / into: / role: …"
    // instead of a hard-to-read flow-inside-flow mapping.
    const blockLines = s.toYamlBlockLines();
    if (blockLines.length > 1) {
      const indentedSelector = blockLines
        .map((line) => `      ${line}`)
        .join("\n");
      const prefix = `- type:\n    into:\n${indentedSelector}\n    text: `;
      const yaml = `${prefix}${quoted}`;
      return {
        yaml,
        placeholders: [
          { offset: prefix.length + 1, length: text.length },
        ],
      };
    }

    const prefix = `- type: { into: ${s.toYamlInline()}, text: `;
    const yaml = `${prefix}${quoted} }`;
    // Position the placeholder inside the quotes so a Monaco snippet
    // selection lands on the bare text, not the surrounding quotes.
    return {
      yaml,
      placeholders: [
        { offset: prefix.length + 1, length: text.length },
      ],
    };
  },
};

const WAIT_FOR: ScriptAction = {
  id: "waitFor",
  label: "Wait for element",
  requiresSelector: true,
  appliesTo: () => true,
  buildYaml: (_node, s) => buildSelectorStep("waitFor", s),
};

const ASSERT_VISIBLE: ScriptAction = {
  id: "assertVisible",
  label: "Assert visible",
  requiresSelector: true,
  appliesTo: () => true,
  buildYaml: (_node, s) => buildSelectorStep("assertVisible", s),
};

const ASSERT_NOT_VISIBLE: ScriptAction = {
  id: "assertNotVisible",
  label: "Assert NOT visible",
  requiresSelector: true,
  appliesTo: () => true,
  buildYaml: (_node, s) => buildSelectorStep("assertNotVisible", s),
};

export const SCRIPT_ACTIONS: readonly ScriptAction[] = [
  TAP,
  TYPE,
  WAIT_FOR,
  ASSERT_VISIBLE,
  ASSERT_NOT_VISIBLE,
];
