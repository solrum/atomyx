import type {
  AttributeBundle,
  AttributeKey,
  EffectiveAttributes,
} from "./theme.types.js";
import { ATTRIBUTE_KEYS } from "./theme.types.js";

/**
 * Root-level fallback bundle for every attribute key. Sits below
 * every theme in the inheritance chain — if no theme in the chain
 * declared a given effect, this table fills it in.
 *
 * Values track IntelliJ Darcula's palette as a rough baseline so
 * the defaults look legible even on a stripped-down theme.
 */
export const DEFAULT_ATTRIBUTES: Readonly<
  Record<AttributeKey, Required<Pick<AttributeBundle, "foreground">> & AttributeBundle>
> = {
  // Editor chrome
  EDITOR_BACKGROUND: { foreground: "#2b2b2b", background: "#2b2b2b" },
  EDITOR_FOREGROUND: { foreground: "#a9b7c6" },
  EDITOR_LINE_HIGHLIGHT_BG: { foreground: "#323232", background: "#323232" },
  EDITOR_SELECTION_BG: { foreground: "#214283", background: "#214283" },
  EDITOR_CARET: { foreground: "#bbbbbb" },
  EDITOR_GUTTER_BG: { foreground: "#313335", background: "#313335" },
  EDITOR_GUTTER_FG: { foreground: "#606366" },

  // Atomyx syntax
  ATOMYX_KEYWORD: { foreground: "#cc7832", fontStyle: "bold" },
  ATOMYX_COMMAND_BARE: { foreground: "#cc7832", fontStyle: "bold" },
  ATOMYX_SELECTOR: { foreground: "#9876aa" },
  ATOMYX_VARIABLE: { foreground: "#9876aa", fontStyle: "italic" },
  ATOMYX_VARIABLE_UNDEFINED: { foreground: "#ff6b68", fontStyle: "italic" },

  // YAML syntax
  SYNTAX_STRING: { foreground: "#6a8759" },
  SYNTAX_NUMBER: { foreground: "#6897bb" },
  SYNTAX_BOOLEAN: { foreground: "#cc7832", fontStyle: "bold" },
  SYNTAX_NULL: { foreground: "#cc7832", fontStyle: "bold" },
  SYNTAX_COMMENT: { foreground: "#808080", fontStyle: "italic" },

  // Diagnostics
  DIAGNOSTIC_ERROR_FG: { foreground: "#ff6b68" },
  DIAGNOSTIC_ERROR_BG: { foreground: "#522e2e", background: "#522e2e" },
  DIAGNOSTIC_WARNING_FG: { foreground: "#bbb529" },
  DIAGNOSTIC_WARNING_BG: { foreground: "#4a4a1e", background: "#4a4a1e" },
  DIAGNOSTIC_INFO_FG: { foreground: "#6897bb" },
  DIAGNOSTIC_HINT_FG: { foreground: "#808080" },

  // Run state
  RUN_STEP_PENDING: { foreground: "#808080" },
  RUN_STEP_RUNNING: { foreground: "#ffc66d" },
  RUN_STEP_PASS: { foreground: "#6a8759" },
  RUN_STEP_FAIL: { foreground: "#ff6b68" },
  RUN_STEP_SKIP: { foreground: "#808080" },
  RUN_STEP_CURRENT_BG: { foreground: "#323232", background: "#323232" },
};

/**
 * Convert an `AttributeKey` to the kebab-case CSS custom property
 * stem, e.g. `ATOMYX_KEYWORD` → `--atomyx-keyword`. Effect
 * channels append their own suffix: `--atomyx-keyword`,
 * `--atomyx-keyword-bg`, `--atomyx-keyword-font-weight`,
 * `--atomyx-keyword-font-italic`.
 */
export function cssVarStem(key: AttributeKey): string {
  return "--" + key.toLowerCase().replace(/_/g, "-");
}

export interface AttributeCssVars {
  readonly foreground: string;
  readonly background: string;
  readonly fontWeight: string;
  readonly fontStyle: string;
}

export function cssVarsFor(key: AttributeKey): AttributeCssVars {
  const stem = cssVarStem(key);
  return {
    foreground: stem,
    background: `${stem}-bg`,
    fontWeight: `${stem}-font-weight`,
    fontStyle: `${stem}-font-italic`,
  };
}

/**
 * Fill in missing effect channels from `DEFAULT_ATTRIBUTES` so
 * downstream consumers (CSS var writer, Monaco translator) can
 * trust every key is present with at least a foreground.
 */
export function fillDefaults(
  partial: Readonly<Partial<Record<AttributeKey, AttributeBundle>>>,
): EffectiveAttributes {
  const result = {} as Record<
    AttributeKey,
    Required<Pick<AttributeBundle, "foreground">> & AttributeBundle
  >;
  for (const key of ATTRIBUTE_KEYS) {
    const declared = partial[key];
    const fallback = DEFAULT_ATTRIBUTES[key];
    result[key] = {
      foreground: declared?.foreground ?? fallback.foreground,
      ...(declared?.background !== undefined
        ? { background: declared.background }
        : fallback.background !== undefined
          ? { background: fallback.background }
          : {}),
      ...(declared?.fontStyle !== undefined
        ? { fontStyle: declared.fontStyle }
        : fallback.fontStyle !== undefined
          ? { fontStyle: fallback.fontStyle }
          : {}),
    };
  }
  return result;
}
