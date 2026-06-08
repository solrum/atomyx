import type * as monaco from "monaco-editor";
import {
  buildCommandRegex,
  buildYamlKeyRegex,
  SCRIPT_COMMAND_NAMES,
} from "../../../domain/features/scripts/index.js";

/**
 * Inline-classes applied to Atomyx editor tokens. The actual
 * colors resolve via CSS custom properties (see
 * `ui/theme/tailwind.css` + `apply-editor-theme.ts`) so a theme
 * swap is a single `setProperty` call — no model / decoration
 * rebuild required.
 */
const KEYWORD_CLASS = "atomyx-keyword";
const COMMAND_BARE_CLASS = "atomyx-command-bare";

const YAML_KEY_REGEX = buildYamlKeyRegex();
const COMMAND_REGEX = buildCommandRegex();
const BARE_COMMAND_NAMES = new Set<string>(SCRIPT_COMMAND_NAMES);

interface TokenMatch {
  readonly prefix: string;
  readonly name: string;
  readonly index: number;
  readonly className: string;
  readonly hoverMessage: string;
}

/**
 * Compute Monaco decorations that inline-highlight every YAML key
 * ("text before `:`") uniformly plus every bare-string command
 * (`- launchApp`, `- back`, `- screenshot`). Two classes, one
 * unified keyword class and one explicitly-bare class, so themes
 * can choose to distinguish them if desired (defaults are equal).
 *
 * Linear scans over document text — fine without debouncing for
 * script sizes typical of Atomyx YAML.
 */
export function computeScriptDecorations(
  model: monaco.editor.ITextModel,
  monacoApi: typeof monaco,
): monaco.editor.IModelDeltaDecoration[] {
  const text = model.getValue();
  const matches: TokenMatch[] = [];

  YAML_KEY_REGEX.lastIndex = 0;
  for (const m of text.matchAll(YAML_KEY_REGEX)) {
    const prefix = m[1] ?? "";
    const name = m[2] ?? "";
    if (!name || m.index === undefined) continue;
    matches.push({
      prefix,
      name,
      index: m.index,
      className: KEYWORD_CLASS,
      hoverMessage: `Atomyx key: \`${name}\``,
    });
  }

  COMMAND_REGEX.lastIndex = 0;
  for (const m of text.matchAll(COMMAND_REGEX)) {
    const prefix = m[1] ?? "";
    const name = m[2] ?? "";
    if (!name || m.index === undefined) continue;
    if (!BARE_COMMAND_NAMES.has(name)) continue;
    const charAfter = text[m.index + prefix.length + name.length];
    if (charAfter === ":") continue;
    matches.push({
      prefix,
      name,
      index: m.index,
      className: COMMAND_BARE_CLASS,
      hoverMessage: `Atomyx command: \`${name}\``,
    });
  }

  return matches.map((tm) => {
    const startOffset = tm.index + tm.prefix.length;
    const endOffset = startOffset + tm.name.length;
    const startPos = model.getPositionAt(startOffset);
    const endPos = model.getPositionAt(endOffset);
    return {
      range: new monacoApi.Range(
        startPos.lineNumber,
        startPos.column,
        endPos.lineNumber,
        endPos.column,
      ),
      options: {
        inlineClassName: tm.className,
        hoverMessage: { value: tm.hoverMessage },
      },
    };
  });
}
