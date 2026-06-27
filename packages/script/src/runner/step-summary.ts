import type {
  ScriptSelector,
  ScriptStep,
} from "@atomyx/shared/script";

/**
 * One coloured token in a step's rendered description. The
 * timeline maps `kind` to a theme role so the run dock visually
 * matches the script editor's syntax highlighting.
 *
 *   - `keyword`   — command name and reserved words
 *   - `identifier` — selector field path, capture variable, etc.
 *   - `string`    — literal user value (selector value, typed text, sleep duration)
 *   - `punct`     — separators (`:`, `=`, `←`, `,`)
 *   - `mask`      — masked secret (e.g. `••••••••` for password input)
 */
export type StepTokenKind =
  | "keyword"
  | "identifier"
  | "string"
  | "punct"
  | "mask";

export interface StepToken {
  readonly kind: StepTokenKind;
  readonly text: string;
}

export interface StepSummary {
  /**
   * One-line plain-text fallback used by logs and consumers that
   * cannot render coloured tokens. Built from the same parts.
   */
  readonly text: string;
  readonly tokens: readonly StepToken[];
}

const SECRET_FIELD_HINTS = [
  "password",
  "passwd",
  "pwd",
  "secret",
  "token",
  "pin",
  "otp",
];

/**
 * Render a step into a coloured token sequence + plain-text
 * fallback, suitable for a progress timeline. Mirrors the
 * editor's syntax styling so the run view reads like a live
 * narration of the script.
 */
export function summarizeStep(step: ScriptStep): StepSummary {
  const builder = new TokenBuilder();
  switch (step.command) {
    case "tap":
      builder.kw("tap").punct(": ").selector(step.selector);
      break;
    case "type": {
      const isSecret = step.into ? selectorLooksSecret(step.into) : false;
      builder.kw("input");
      if (step.into) {
        builder.punct(": ").selector(step.into).punct(" ← ");
      } else {
        builder.punct(": ");
      }
      if (isSecret) {
        builder.mask(maskOf(step.text.length));
      } else {
        builder.str(step.text);
      }
      break;
    }
    case "waitFor":
      builder.kw("waitFor").punct(": ").selector(step.selector);
      if (step.timeoutMs !== undefined) {
        builder.punct(" (").id("timeout").punct("=").str(`${step.timeoutMs}ms`).punct(")");
      }
      break;
    case "assertVisible":
      builder.kw("assertVisible").punct(": ").selector(step.selector);
      break;
    case "assertNotVisible":
      builder.kw("assertNotVisible").punct(": ").selector(step.selector);
      break;
    case "screenshot":
      builder.kw("screenshot");
      if (step.label) builder.punct(": ").str(step.label);
      break;
    case "swipe":
      builder.kw("swipe").punct(": ").str(step.direction);
      break;
    case "pressKey":
      builder.kw("pressKey").punct(": ").str(step.key);
      break;
    case "sleep":
      builder.kw("sleep").punct(": ").str(`${step.ms}ms`);
      break;
    case "back":
      builder.kw("back");
      break;
    case "launchApp":
      builder.kw("launchApp");
      break;
    case "capture": {
      builder
        .kw("capture")
        .punct(": ")
        .id(step.as)
        .punct(" ← ")
        .str(step.pattern);
      break;
    }
    case "handle": {
      const count = step.branches?.length ?? 0;
      builder
        .kw("handle")
        .punct(": ")
        .str(`${count} branch${count === 1 ? "" : "es"}`);
      if (step.timeout) {
        builder
          .punct(" (")
          .id("timeout")
          .punct("=")
          .str(`${step.timeout}ms`)
          .punct(")");
      }
      break;
    }
    case "branch": {
      const cases = step.on?.length ?? 0;
      builder
        .kw("branch")
        .punct(" on ")
        .id(`$${step.from}`)
        .punct(" (")
        .str(`${cases} case${cases === 1 ? "" : "s"}`)
        .punct(")");
      break;
    }
    case "runFlow":
      builder.kw("runFlow").punct(": ").str(step.file);
      break;
    case "assertApi": {
      builder.kw("assertApi").punct(": ").id(`$${step.from}`);
      const parts: string[] = [];
      if (step.status !== undefined) parts.push(`status=${step.status}`);
      if (step.body && Object.keys(step.body).length > 0) {
        const n = Object.keys(step.body).length;
        parts.push(`body×${n}`);
      }
      if (parts.length > 0) builder.punct(" → ").rawStr(parts.join(", "));
      break;
    }
    case "extract": {
      const valueKeys = step.values ? Object.keys(step.values) : [];
      builder
        .kw("extract")
        .punct(": ")
        .id(`$${step.from}`)
        .punct(" → ")
        .rawStr(valueKeys.length > 0 ? valueKeys.join(", ") : "?");
      break;
    }
    default:
      builder.kw(step.command);
      break;
  }
  return builder.build();
}

class TokenBuilder {
  private readonly tokens: StepToken[] = [];

  kw(text: string): this {
    return this.push("keyword", text);
  }
  id(text: string): this {
    return this.push("identifier", text);
  }
  str(text: string): this {
    return this.push("string", `"${truncate(text, 48)}"`);
  }
  rawStr(text: string): this {
    return this.push("string", truncate(text, 48));
  }
  punct(text: string): this {
    return this.push("punct", text);
  }
  mask(text: string): this {
    return this.push("mask", text);
  }
  selector(selector: ScriptSelector | undefined): this {
    if (!selector) return this.id("element").punct(".").id("?");
    const field = selectorFieldName(selector);
    const value = field ? selector[field] : undefined;
    if (field === undefined || value === undefined) {
      return this.id("element").punct(".").id("?");
    }
    return this.id("element")
      .punct(".")
      .id(field)
      .punct(" = ")
      .str(String(value));
  }

  private push(kind: StepTokenKind, text: string): this {
    this.tokens.push({ kind, text });
    return this;
  }

  build(): StepSummary {
    return {
      tokens: this.tokens,
      text: this.tokens.map((t) => t.text).join(""),
    };
  }
}

type SelectorStringField = "id" | "text" | "label" | "hint" | "role";

function selectorFieldName(
  selector: ScriptSelector,
): SelectorStringField | undefined {
  if (selector.id !== undefined) return "id";
  if (selector.text !== undefined) return "text";
  if (selector.label !== undefined) return "label";
  if (selector.hint !== undefined) return "hint";
  if (selector.role !== undefined) return "role";
  return undefined;
}

function looksSecret(fieldValue: string): boolean {
  const lower = fieldValue.toLowerCase();
  return SECRET_FIELD_HINTS.some((hint) => lower.includes(hint));
}

function selectorLooksSecret(selector: ScriptSelector): boolean {
  const candidates = [
    selector.id,
    selector.label,
    selector.hint,
    selector.text,
  ].filter((v): v is string => typeof v === "string");
  return candidates.some(looksSecret);
}

function maskOf(length: number): string {
  const n = Math.max(4, Math.min(12, length));
  return "•".repeat(n);
}

function truncate(value: string, max: number): string {
  return value.length > max ? `${value.slice(0, max - 1)}…` : value;
}
