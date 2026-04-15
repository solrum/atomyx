/**
 * Variable substitution for YAML specs.
 *
 * Supported patterns:
 *   ${data.key}         — from spec.data
 *   ${env.X}            — from process.env
 *   ${env.X:-default}   — from process.env with default
 *   ${data.x.y.z}       — nested path traversal
 *
 * Walks the entire object recursively, substitutes in all string values.
 */

const VAR_RE = /\$\{([^}]+)\}/g;

export interface ResolveContext {
  data?: Record<string, unknown>;
  env?: Record<string, string | undefined>;
}

function getPath(obj: unknown, path: string): unknown {
  let current: any = obj;
  for (const key of path.split(".")) {
    if (current == null) return undefined;
    current = current[key];
  }
  return current;
}

function resolveExpr(expr: string, ctx: ResolveContext): string {
  // Split default: "env.X:-fallback"
  const colonDash = expr.indexOf(":-");
  let path = expr;
  let fallback: string | undefined;
  if (colonDash >= 0) {
    path = expr.slice(0, colonDash);
    fallback = expr.slice(colonDash + 2);
  }

  if (path.startsWith("data.")) {
    const v = getPath(ctx.data ?? {}, path.slice(5));
    if (v != null) return String(v);
  } else if (path.startsWith("env.")) {
    const v = (ctx.env ?? process.env)[path.slice(4)];
    if (v != null) return v;
  }

  if (fallback != null) return fallback;
  throw new Error(`unresolved variable: \${${expr}}`);
}

export function resolveString(str: string, ctx: ResolveContext): string {
  return str.replace(VAR_RE, (_match, expr) => resolveExpr(expr, ctx));
}

export function resolveDeep<T>(value: T, ctx: ResolveContext): T {
  if (value == null) return value;
  if (typeof value === "string") {
    return resolveString(value, ctx) as unknown as T;
  }
  if (Array.isArray(value)) {
    return value.map((v) => resolveDeep(v, ctx)) as unknown as T;
  }
  if (typeof value === "object") {
    const out: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(value)) {
      out[k] = resolveDeep(v, ctx);
    }
    return out as unknown as T;
  }
  return value;
}
