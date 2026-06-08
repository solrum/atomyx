import * as monaco from "monaco-editor";
import type {
  EffectiveAttributes,
  MonacoBaseTheme,
} from "../../../domain/features/theme/index.js";

const ATOMYX_THEME_NAME = "atomyx-active";

function fontStyleMonaco(bundle: EffectiveAttributes[keyof EffectiveAttributes]):
  | { fontStyle: string }
  | undefined {
  const style = bundle.fontStyle;
  if (!style || style === "normal") return undefined;
  const parts: string[] = [];
  if (style === "bold" || style === "bold-italic") parts.push("bold");
  if (style === "italic" || style === "bold-italic") parts.push("italic");
  if (parts.length === 0) return undefined;
  return { fontStyle: parts.join(" ") };
}

function hex(attr: EffectiveAttributes[keyof EffectiveAttributes]): string {
  return attr.foreground;
}

function hexBg(
  attr: EffectiveAttributes[keyof EffectiveAttributes],
  fallback: string,
): string {
  return attr.background ?? fallback;
}

/**
 * Translate the active attribute map into a Monaco theme and
 * activate it. Called on every `effective` change from the theme
 * store.
 *
 * Monaco's theme model wants:
 *   - `rules: [{ token, foreground, background?, fontStyle? }]`
 *     for syntax highlighting by token kind.
 *   - `colors: { "editor.background": "#RRGGBB", ... }` for
 *     chrome.
 *
 * We map the SYNTAX_* + EDITOR_* attributes onto these. The
 * ATOMYX_* attributes drive the Atomyx decoration classes and
 * flow through CSS variables, not the Monaco theme.
 */
export function applyMonacoTheme(
  effective: EffectiveAttributes,
  base: MonacoBaseTheme,
): void {
  const rules: monaco.editor.ITokenThemeRule[] = [];

  const push = (token: string, attr: EffectiveAttributes[keyof EffectiveAttributes]) => {
    const fg = attr.foreground.replace("#", "");
    const rule: monaco.editor.ITokenThemeRule = { token, foreground: fg };
    const extra = fontStyleMonaco(attr);
    if (extra) rule.fontStyle = extra.fontStyle;
    rules.push(rule);
  };

  push("string", effective.SYNTAX_STRING);
  push("string.yaml", effective.SYNTAX_STRING);
  push("number", effective.SYNTAX_NUMBER);
  push("number.yaml", effective.SYNTAX_NUMBER);
  push("keyword", effective.ATOMYX_KEYWORD);
  push("keyword.yaml", effective.ATOMYX_KEYWORD);
  push("comment", effective.SYNTAX_COMMENT);
  push("comment.yaml", effective.SYNTAX_COMMENT);
  push("type", effective.SYNTAX_BOOLEAN);
  push("constant", effective.SYNTAX_BOOLEAN);

  const editorBg = hexBg(effective.EDITOR_BACKGROUND, "#2b2b2b");
  const editorFg = hex(effective.EDITOR_FOREGROUND);
  const selectionBg = hexBg(effective.EDITOR_SELECTION_BG, "#214283");
  const lineBg = hexBg(effective.EDITOR_LINE_HIGHLIGHT_BG, editorBg);
  const caret = hex(effective.EDITOR_CARET);
  const gutterBg = hexBg(effective.EDITOR_GUTTER_BG, editorBg);
  const gutterFg = hex(effective.EDITOR_GUTTER_FG);

  monaco.editor.defineTheme(ATOMYX_THEME_NAME, {
    base,
    inherit: true,
    rules,
    colors: {
      "editor.background": editorBg,
      "editor.foreground": editorFg,
      "editor.selectionBackground": selectionBg,
      "editor.lineHighlightBackground": lineBg,
      "editorCursor.foreground": caret,
      "editorGutter.background": gutterBg,
      "editorLineNumber.foreground": gutterFg,
      "editorError.foreground": hex(effective.DIAGNOSTIC_ERROR_FG),
      "editorWarning.foreground": hex(effective.DIAGNOSTIC_WARNING_FG),
      "editorInfo.foreground": hex(effective.DIAGNOSTIC_INFO_FG),
    },
  });
  monaco.editor.setTheme(ATOMYX_THEME_NAME);
}
